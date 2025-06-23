package repo

import (
	"context"
	"database/sql"
	"fmt"
	"net/netip"
	"strings"
	"sync"
	"time"

	"ired.com/olt/models"
	"ired.com/olt/utils"
)

type oltInfo struct {
	Id       string
	Nombre   string
	Ip       netip.Addr
	Username string
	Passwd   string
	ItemId   sql.NullString
}

type clockResult struct {
	ItemId string
	Value  string
}

// Main Function for cron icmp_pinger
func GetClock(db models.ConnDb, caller string) error {
	//show status of worker
	utils.Logline(utils.ShowStatusWorker(db, "getClock", caller+"/begin"))

	query := `SELECT h.id, h.nombre, h.ip, h.info->>'telnet_username' as username, h.info->>'telnet_password' as passwd, hi.id as item_id
		FROM network.host as h
		LEFT JOIN network.host_item as hi ON hi.host_id=h.id AND hi.nombre='olt-clock'
		WHERE h.info->>'telnet_password' IS NOT NULL AND h.info->>'telnet_username' IS NOT NULL AND h.activo=true
		ORDER BY RANDOM()
	`

	rows, err := db.Conn.Query(db.Ctx, query)
	if err != nil {
		utils.Logline("error getting hosts - olts", err)
		return err
	}
	defer rows.Close()

	var hostsData []oltInfo
	for rows.Next() {
		var host oltInfo
		err = rows.Scan(&host.Id, &host.Nombre, &host.Ip, &host.Username, &host.Passwd, &host.ItemId)
		if err != nil {
			utils.Logline("error scanning rows of host olts", err)
			return err
		}
		hostsData = append(hostsData, host)
	}
	rows.Close()

	var wg sync.WaitGroup
	results := make(chan clockResult, 20)
	for _, host := range hostsData {
		wg.Add(1)
		if host.Username == "vsol" {
			go workerVsolClock(&wg, db, host.Ip, hostsData, results)
		} else if host.Username == "cdata" {
			go workerCdataClock(&wg, db, host.Ip, hostsData, results)
		} else {
			go workerZteClock(&wg, db, host.Ip, hostsData, results)
		}
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	err = insertDbClock(db, results)
	if err != nil {
		utils.Logline("error inserting data", "getClock", err)
	}

	//show status of worker
	utils.Logline(utils.ShowStatusWorker(db, "getClock", caller+"/ending"))

	return nil
}

func workerZteClock(wg *sync.WaitGroup, db models.ConnDb, hostIp netip.Addr, hosts []oltInfo, results chan<- clockResult) {
	defer wg.Done()

	defer func() {
		// recover from panic if one occured. Set err to nil otherwise.
		if recover() != nil {
			utils.Logline("error on this subprocess - workerZteClock", hostIp.String())
			return
		}
	}()

	hostInfo := findOltInfoByIp(hosts, hostIp.String())
	if hostInfo == nil {
		utils.Logline("error finding host info for ip", hostIp.String())
		return
	}

	// Connect to the OLT
	conn, err := utils.OltZteConnect(hostInfo.Ip.String(), "23", hostInfo.Username, hostInfo.Passwd)
	if err != nil {
		utils.Logline("Couldnt establish connection", err)
		return
	}
	defer conn.Close()

	//send command and read response
	var response string
	if response, err = utils.OltZteSend(conn, "show clock", "#", 2*time.Second); err != nil {
		utils.Logline("error proccesing 'show clock'", err)
		return
	}
	conn.Close()

	//clean response and store it
	dateOlt := cleanZteDate(response)

	// Parse the string into a time.Time object
	loc, err := time.LoadLocation("America/Caracas")
	if err != nil {
		utils.Logline("Error loading location:", hostInfo.Ip.String(), err)
		return
	}

	// Define the layout for parsing
	layout := "15:04:05 Mon Jan 02 2006"
	t, err := time.ParseInLocation(layout, dateOlt[:24], loc)
	if err != nil {
		utils.Logline("Error parsing date:", hostInfo.Ip.String(), err)
		return
	}

	//create json string to save it in DB
	horaPgsql := fmt.Sprintf(`{"olt_format": "%s", "timestamptz_servidor": "%s", "timestamptz_olt": "%s"}`, dateOlt, time.Now().Local().Format(time.RFC3339), t.Format(time.RFC3339))

	//validar si itemId existe sino crearlo
	itemId := hostInfo.ItemId.String
	if !hostInfo.ItemId.Valid {
		itemId = createItem(db, hostInfo.Id, hostInfo.Ip, "telnet-show-clock", "olt-clock")
	}

	// si itemId es correcto concatenar para insertar
	if itemId != "" {
		results <- clockResult{ItemId: itemId, Value: horaPgsql}
	}
}

func workerVsolClock(wg *sync.WaitGroup, db models.ConnDb, hostIp netip.Addr, hosts []oltInfo, results chan<- clockResult) {
	defer wg.Done()

	defer func() {
		// recover from panic if one occured. Set err to nil otherwise.
		if recover() != nil {
			utils.Logline("error on this subprocess - workerVsolClock", hostIp.String())
			return
		}
	}()

	hostInfo := findOltInfoByIp(hosts, hostIp.String())
	if hostInfo == nil {
		utils.Logline("error finding host info for ip", hostIp.String())
		return
	}

	// Connect to the OLT
	conn, err := utils.OltVsolConnect(hostInfo.Ip.String(), "23", hostInfo.Username, hostInfo.Passwd)
	if err != nil {
		utils.Logline("Couldnt establish connection", err)
		return
	}
	defer conn.Close()

	//send command and read response
	var response string
	if response, err = utils.OltZteSend(conn, "show time", "#", 2*time.Second); err != nil {
		utils.Logline("error proccesing 'show clock'", err)
		return
	}
	conn.Close()

	//clean response and parse it
	response = cleanZteDate(response)
	for _, line := range strings.Split(response, "\n") {
		if strings.Contains(line, "current date") {
			response = strings.TrimSpace(strings.ReplaceAll(strings.ToLower(line), "the current date/time is :", ""))
		}
	}

	// Parse the string into a time.Time object
	loc, err := time.LoadLocation("America/Caracas")
	if err != nil {
		utils.Logline("Error loading location:", hostIp.String(), err)
		return
	}

	// Define the layout for parsing
	layout := "Mon Jan 02 15:04:05 2006"
	t, err := time.ParseInLocation(layout, response, loc)
	if err != nil {
		utils.Logline("Error parsing date:", hostIp.String(), err)
		return
	}

	//create json string to save it in DB
	horaPgsql := fmt.Sprintf(`{"olt_format": "%s", "timestamptz_servidor": "%s", "timestamptz_olt": "%s"}`, response, time.Now().Local().Format(time.RFC3339), t.Format(time.RFC3339))

	//validar si itemId existe sino crearlo
	itemId := hostInfo.ItemId.String
	if !hostInfo.ItemId.Valid {
		itemId = createItem(db, hostInfo.Id, hostInfo.Ip, "telnet-show-clock", "olt-clock")
	}

	// si itemId es correcto concatenar para insertar
	if itemId != "" {
		results <- clockResult{ItemId: itemId, Value: horaPgsql}
	}
}

func workerCdataClock(wg *sync.WaitGroup, db models.ConnDb, hostIp netip.Addr, hosts []oltInfo, results chan<- clockResult) {
	defer wg.Done()

	defer func() {
		// recover from panic if one occured. Set err to nil otherwise.
		if recover() != nil {
			utils.Logline("error on this subprocess - workerCdataClock", hostIp.String())
			return
		}
	}()

	hostInfo := findOltInfoByIp(hosts, hostIp.String())
	if hostInfo == nil {
		utils.Logline("error finding host info for ip", hostIp.String())
		return
	}

	// Connect to the OLT
	conn, err := utils.OltCdataConnect(hostInfo.Ip.String(), "23", hostInfo.Username, hostInfo.Passwd)
	if err != nil {
		utils.Logline("Couldnt establish connection", err)
		return
	}
	defer conn.Close()

	//send command and read response
	var response string
	if response, err = utils.OltZteSend(conn, "show time", "#", 2*time.Second); err != nil {
		utils.Logline("error proccesing 'show clock'", err)
		return
	}
	conn.Close()

	//clean response and parse it
	response = strings.TrimSpace(utils.OltRemoveLastLine(response))
	for _, line := range strings.Split(response, "\n") {
		if !strings.Contains(line, "show time") {
			response = strings.TrimSpace(line)
		}
	}

	// Parse the string into a time.Time object
	loc, err := time.LoadLocation("America/Caracas")
	if err != nil {
		utils.Logline("Error loading location:", hostIp.String(), err)
		return
	}

	// Define the layout for parsing
	layout := "2006-01-02 15:04:05"
	t, err := time.ParseInLocation(layout, response[:19], loc)
	if err != nil {
		utils.Logline("Error parsing date:", hostIp.String(), err)
		return
	}

	//create json string to save it in DB
	horaPgsql := fmt.Sprintf(`{"olt_format": "%s", "timestamptz_servidor": "%s", "timestamptz_olt": "%s"}`, response, time.Now().Local().Format(time.RFC3339), t.Format(time.RFC3339))

	//validar si itemId existe sino crearlo
	itemId := hostInfo.ItemId.String
	if !hostInfo.ItemId.Valid {
		itemId = createItem(db, hostInfo.Id, hostInfo.Ip, "telnet-show-clock", "olt-clock")
	}

	// si itemId es correcto concatenar para insertar
	if itemId != "" {
		results <- clockResult{ItemId: itemId, Value: horaPgsql}
	}
}

func createItem(db models.ConnDb, hostId string, hostIp netip.Addr, itemSn string, itemName string) (itemId string) {
	query := `WITH ins AS (
			INSERT INTO network.host_item (empresa_id, activo, host_id, sn, ip, nombre)
			SELECT 1, true, $1, $2, $3, $4
			WHERE NOT EXISTS (SELECT id FROM network.host_item WHERE host_id = $1 AND nombre=$4)
			RETURNING id
		), upd as (
			UPDATE network.host_item SET activo=true, ip=$3
			WHERE host_id = $1 AND nombre=$4
			RETURNING id
		)
		SELECT id FROM ins
		UNION ALL
		SELECT id FROM upd
		UNION ALL
		SELECT id FROM network.host_item WHERE host_id = $1 AND nombre=$4
		LIMIT 1`

	if err := db.Conn.QueryRow(db.Ctx, query, hostId, itemSn, hostIp, itemName).Scan(&itemId); err != nil {
		utils.Logline("error inserting network.host_item ("+itemName+")", hostId, hostIp.String(), err)
		return ""
	}

	return itemId
}

// funcion para insertar data a DB
func insertDbClock(db models.ConnDb, results chan clockResult) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	//open a transaction
	tx, err := db.Conn.Begin(ctx)
	if err != nil {
		utils.Logline("error starting transaction", "getClock", err)
		return err
	}
	defer func() {
		if err != nil {
			tx.Rollback(ctx) // Rollback if there's an error
			utils.Logline("transaction rolled back due to error", "getClock", err)
		}
	}()

	//process results chan
	var count int
	for item := range results {
		queryInternal := "INSERT INTO estadistica.detalle_text(item_id, value) VALUES ($1, $2)"
		_, err = tx.Exec(ctx, queryInternal, item.ItemId, item.Value)
		if err != nil {
			return err
		}
		count++
	}

	//commit transaction
	err = tx.Commit(ctx)
	if err != nil {
		return err
	}

	utils.Logline(fmt.Sprintf("(%d) records inserted on estadistica.detalle_text", count), "getClock")

	return nil
}

// funcion para buscar en un arreglo de tipo queueItem el itemId por IP
func findOltInfoByIp(hosts []oltInfo, targetIp string) *oltInfo {
	for _, item := range hosts {
		if item.Ip.String() == targetIp {
			return &item
		}
	}
	return nil
}

func cleanZteDate(s string) string {
	dateOlt := strings.TrimSpace(utils.OltRemoveLastLine(s))
	dateOlt = strings.ReplaceAll(dateOlt, " 1 ", " 01 ")
	dateOlt = strings.ReplaceAll(dateOlt, " 2 ", " 02 ")
	dateOlt = strings.ReplaceAll(dateOlt, " 3 ", " 03 ")
	dateOlt = strings.ReplaceAll(dateOlt, " 4 ", " 04 ")
	dateOlt = strings.ReplaceAll(dateOlt, " 5 ", " 05 ")
	dateOlt = strings.ReplaceAll(dateOlt, " 6 ", " 06 ")
	dateOlt = strings.ReplaceAll(dateOlt, " 7 ", " 07 ")
	dateOlt = strings.ReplaceAll(dateOlt, " 8 ", " 08 ")
	dateOlt = strings.ReplaceAll(dateOlt, " 9 ", " 09 ")

	return dateOlt
}

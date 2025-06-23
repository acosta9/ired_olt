package repo

import (
	"context"
	"fmt"
	"net/netip"
	"regexp"
	"strings"
	"sync"
	"time"

	"ired.com/olt/models"
	"ired.com/olt/utils"
)

type hostItems struct {
	HostId     string
	ItemId     string
	ItemSn     string
	ItemNombre string
}

func OltInfo(db models.ConnDb, caller string) error {
	//show status of worker
	utils.Logline(utils.ShowStatusWorker(db, "oltInfo", caller+"/begin"))

	//get hostItems and storeIt in a slice
	query := `SELECT h.id, hi.id as item_id, hi.sn as sn, hi.nombre
		FROM network.host as h
		LEFT JOIN network.host_item as hi ON hi.host_id=h.id
		WHERE h.info->>'telnet_username' IS NOT NULL AND h.info->>'snmp_read_community' IS NOT NULL AND h.activo=true AND hi.nombre LIKE 'olt-%' AND hi.nombre<>'olt-clock'
		ORDER BY h.ip ASC, hi.nombre ASC`
	rows, err := db.Conn.Query(db.Ctx, query)
	if err != nil {
		utils.Logline("error getting host - items from olts", err)
		return err
	}
	defer rows.Close()

	var items []hostItems
	for rows.Next() {
		var item hostItems
		err = rows.Scan(&item.HostId, &item.ItemId, &item.ItemSn, &item.ItemNombre)
		if err != nil {
			utils.Logline("error scanning rows of host_items from olts", err)
			return err
		}
		items = append(items, item)
	}
	rows.Close()

	//get olts to work on
	query = `SELECT h.id, h.ip, h.info->>'telnet_username' as username, h.info->>'telnet_password' as password, h.info->>'snmp_read_community' as community
		FROM network.host as h
		WHERE h.info->>'telnet_username' IS NOT NULL AND h.info->>'snmp_read_community' IS NOT NULL AND h.activo=true
		ORDER BY RANDOM()`
	if rows, err = db.Conn.Query(db.Ctx, query); err != nil {
		utils.Logline("error getting host to run cron", err)
		return err
	}
	defer rows.Close()

	//create slice of hosts
	var hostsInfo []models.HostInfo
	for rows.Next() {
		var host models.HostInfo
		err = rows.Scan(&host.Id, &host.Ip, &host.TelnetUsername, &host.TelnetPasswd, &host.SnmpCommunity)
		if err != nil {
			utils.Logline("error scanning rows of host olts", err)
			return err
		}
		hostsInfo = append(hostsInfo, host)
	}
	rows.Close()

	//iterate over hosts and create one goroutine for every olt
	var wg sync.WaitGroup
	for _, host := range hostsInfo {
		wg.Add(1)
		go func() {
			// wg.Done()
			if host.TelnetUsername == "vsol" {
				workerVsolInfo(&wg, db, host, items)
			} else if host.TelnetUsername == "cdata" {
				workerCdataInfo(&wg, db, host, items)
			} else {
				workerZteInfo(&wg, db, host, items)
			}
		}()
	}

	wg.Wait()

	//show status of worker
	utils.Logline(utils.ShowStatusWorker(db, "oltInfo", caller+"/ending"))

	return nil
}

func workerZteInfo(wg *sync.WaitGroup, db models.ConnDb, host models.HostInfo, items []hostItems) {
	defer wg.Done()

	defer func() {
		// recover from panic if one occured. Set err to nil otherwise.
		if recover() != nil {
			utils.Logline("error on this subprocess - workerZteInfo", host.Ip.String())
			return
		}
	}()

	// Create a context with a timeout
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	//crear canal para recibir la respuesta de las operaciones en snmp
	errChan := make(chan error, 1)
	resultChan := make(chan []models.ItemResult, 1)

	go func() {
		var err error
		var result []models.ItemResult
		//connect to snmp
		connSnmp, err := utils.OltSnmpConnect(host.Ip.String(), host.SnmpCommunity, 4, 4, true)
		if err != nil {
			utils.Logline("Couldnt establish connection", err)
			errChan <- err
			return
		}
		defer connSnmp.Conn.Close()

		//get cards info
		modeloOlt := "c320"
		oidsBulk := []string{
			// ".1.3.6.1.4.1.3902.1082.10.1.2.4.1", //boardAllOids
			".1.3.6.1.4.1.3902.1082.10.1.2.4.1.4", //boardType
			".1.3.6.1.4.1.3902.1082.10.1.2.4.1.5", //boardStatus
			// 1 inService
			// 2 notInService
			// 3.hwOnline
			// 4 hwOffline
			// 5 configuring
			// 6 configFailed
			// 7 MIB value Mismatch
			// 8 deactived
			// 9 faulty
			// 10 invalid
			// 11 noPower
			".1.3.6.1.4.1.3902.1082.10.1.2.4.1.9", //boardCpu
		}
		for _, oid := range oidsBulk {
			resultSnmp, err := connSnmp.BulkWalkAll(oid)
			if err != nil {
				utils.Logline("Error performing BulkWalk: ", host.Ip.String(), oid, err)
				errChan <- err
				return
			}
			for _, value := range resultSnmp {
				var itemId, cardSn, cardValue, cardNum, itemTable string

				oid := value.Name
				cardSn = strings.TrimSpace(strings.ReplaceAll(oid, ".1.3.6.1.4.1.3902.1082.10.1.2.4", ""))

				cardNumArr := strings.Split(cardSn, ".")
				cardNum = cardNumArr[len(cardNumArr)-1]

				item := findHostItemSn(items, host.Id, cardSn)
				if item != nil {
					itemId = item.ItemId
				}

				switch {
				case strings.Contains(oid, ".1.3.6.1.4.1.3902.1082.10.1.2.4.1.4."):
					itemTable = "detalle_text"
					cardValue = strings.TrimSpace(strings.ToLower(fmt.Sprintf("%s", value.Value)))
					// se determina en relacion al tipo de tarjetas que modelo de OLT es
					if cardValue == "prwg" {
						modeloOlt = "c300"
					} else if (cardValue == "scxn" || cardValue == "scxm") && modeloOlt != "c300" {
						modeloOlt = "c300 mini"
					}
					//si itemId no existe crearlo
					itemName := "olt-card-type-" + cardNum
					if len(itemId) <= 1 || itemName != item.ItemNombre {
						itemId = createItemSnmp(db, host.Id, host.Ip, cardSn, itemName)
					}
				case strings.Contains(oid, ".1.3.6.1.4.1.3902.1082.10.1.2.4.1.5."):
					itemTable = "detalle_int"
					cardValue = fmt.Sprintf("%d", value.Value)
					//si itemId no existe crearlo
					itemName := "olt-card-status-" + cardNum
					if len(itemId) <= 1 || itemName != item.ItemNombre {
						itemId = createItemSnmp(db, host.Id, host.Ip, cardSn, itemName)
					}
				case strings.Contains(oid, ".1.3.6.1.4.1.3902.1082.10.1.2.4.1.9."):
					itemTable = "detalle_int"
					cardValue = fmt.Sprintf("%d", value.Value)
					//si itemId no existe crearlo
					itemName := "olt-card-cpuload-" + cardNum
					if len(itemId) <= 1 || itemName != item.ItemNombre {
						itemId = createItemSnmp(db, host.Id, host.Ip, cardSn, itemName)
					}
				}

				if cardValue != "" && itemId != "" {
					result = append(result, models.ItemResult{ItemId: itemId, Value: cardValue, Table: itemTable})
				}
			}
		}

		//get fan speed
		oidsBulk = []string{
			".1.3.6.1.4.1.3902.1082.10.10.2.4.11.1.7", //fanSpeed
		}
		for _, oid := range oidsBulk {
			resultSnmp, err := connSnmp.BulkWalkAll(oid)
			if err != nil {
				utils.Logline("Error performing BulkWalk: ", host.Ip.String(), oid, err)
				errChan <- err
				return
			}
			for _, value := range resultSnmp {
				var itemId string

				fanSpeedSn := strings.TrimSpace(strings.ReplaceAll(value.Name, ".1.3.6.1.4.1.3902.1082.10.10.2.4.11.1", ""))
				item := findHostItemSn(items, host.Id, fanSpeedSn)
				if item != nil {
					itemId = item.ItemId
				}

				fanNumArr := strings.Split(fanSpeedSn, ".")
				fanNum := fanNumArr[len(fanNumArr)-1]

				//si itemId no existe crearlo
				itemName := "olt-fan-" + fanNum
				if len(itemId) <= 1 || itemName != item.ItemNombre {
					itemId = createItemSnmp(db, host.Id, host.Ip, fanSpeedSn, itemName)
				}

				fanSpeed := fmt.Sprintf("%d", value.Value)
				result = append(result, models.ItemResult{ItemId: itemId, Value: fanSpeed, Table: "detalle_int"})
			}
		}

		//get olt general info
		oids := []string{
			".1.3.6.1.2.1.1.1.0",                         //modelo
			".1.3.6.1.4.1.3902.1082.10.10.2.1.5.1.3.1.1", //temperature
			".1.3.6.1.2.1.1.3.0",                         //uptime
		}
		resultSnmp, err := connSnmp.Get(oids)
		if err != nil {
			utils.Logline("Error performing Get for OIDs", host.Ip.String(), oids, err)
			errChan <- err
			return
		}
		for _, value := range resultSnmp.Variables {
			var itemId, itemValue, itemTable string
			oid := value.Name

			item := findHostItemSn(items, host.Id, oid)
			if item != nil {
				itemId = item.ItemId
			}

			switch oid {
			case ".1.3.6.1.2.1.1.1.0":
				modelo := strings.ToLower(fmt.Sprintf("%s", value.Value))
				modelo = strings.ReplaceAll(modelo, ", copyright (c) by zte corporation compiled", "")
				modelo = strings.TrimSpace(strings.ReplaceAll(modelo, "software", ""))
				if modeloOlt == "c300 mini" {
					modelo = strings.ReplaceAll(modelo, "c300", "c300 mini")
				}

				if len(itemId) <= 1 {
					itemId = createItemSnmp(db, host.Id, host.Ip, oid, "olt-devmodel")
				}
				itemValue = modelo
				itemTable = "detalle_text"
			case ".1.3.6.1.4.1.3902.1082.10.10.2.1.5.1.3.1.1":
				temperatura := fmt.Sprintf("%d", value.Value)
				if len(itemId) <= 1 {
					itemId = createItemSnmp(db, host.Id, host.Ip, oid, "olt-temperature")
				}
				itemValue = temperatura
				itemTable = "detalle_int"
			case ".1.3.6.1.2.1.1.3.0":
				uptime := fmt.Sprintf("%d", value.Value)
				if len(itemId) <= 1 {
					itemId = createItemSnmp(db, host.Id, host.Ip, oid, "olt-uptime")
				}
				itemValue = uptime
				itemTable = "detalle_int"
			}

			if itemValue != "" && itemId != "" {
				result = append(result, models.ItemResult{ItemId: itemId, Value: itemValue, Table: itemTable})
			}
		}

		connSnmp.Conn.Close()

		resultChan <- result
	}()

	//wait for response on errChan or resultChan
	select {
	case <-ctx.Done():
		// Timeout occurred
		utils.Logline("timeout occurred while processing snmp on", host.Ip.String(), ctx.Err())
		return
	case err := <-errChan:
		if err != nil {
			utils.Logline("error processing snmp on", host.Ip.String(), err)
			return
		}
	case response := <-resultChan:
		if err := insertEstadistica(db, response, host.Ip.String(), "get_olt_info"); err != nil {
			utils.Logline("error inserting data", host.Ip.String(), err)
			return
		}
	}
}

func workerCdataInfo(wg *sync.WaitGroup, db models.ConnDb, host models.HostInfo, items []hostItems) {
	defer wg.Done()

	defer func() {
		// recover from panic if one occured. Set err to nil otherwise.
		if recover() != nil {
			utils.Logline("error on this subprocess - workerCdataInfo", host.Ip.String())
			return
		}
	}()

	// Create a context with a timeout
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	//crear canal para recibir la respuesta de las operaciones en snmp y telnet
	errChan := make(chan error, 1)
	resultChan := make(chan []models.ItemResult, 1)

	go func() {
		var itemId, response string
		var err error
		var result []models.ItemResult

		// Connect to the OLT via telnet to get temp, cpu and fan
		connTelnet, err := utils.OltCdataConnect(host.Ip.String(), "23", host.TelnetUsername, host.TelnetPasswd)
		if err != nil {
			utils.Logline("Couldnt establish connection", err)
			errChan <- err
			return
		}
		defer connTelnet.Close()

		//send command and read response - get temperature
		itemId = ""
		if response, err = utils.OltZteSend(connTelnet, "show temperature", "#", 2*time.Second); err != nil {
			utils.Logline("error proccesing 'show temperature'", host.Ip.String(), err)
			errChan <- err
			return
		}
		response = strings.TrimSpace(utils.OltRemoveLastLine(response))
		for _, line := range strings.Split(response, "\n") {
			if strings.Contains(line, "board") {
				response = strings.ReplaceAll(line, "the temperature of the board:", "")
				response = strings.TrimSpace(strings.ReplaceAll(response, "(c)", ""))

				item := findHostItemSn(items, host.Id, ".1.3.6.1.4.1.3902.1082.10.10.2.1.5.1.3.1.1") //use oid of zte olt
				if item != nil {
					itemId = item.ItemId
				} else {
					itemId = createItemSnmp(db, host.Id, host.Ip, ".1.3.6.1.4.1.3902.1082.10.10.2.1.5.1.3.1.1", "olt-temperature")
				}

				if itemId != "" && response != "" {
					result = append(result, models.ItemResult{ItemId: itemId, Value: response, Table: "detalle_int"})
				}
			}
		}

		//send command and read response - get cpu usage 1min
		itemId = ""
		if response, err = utils.OltZteSend(connTelnet, "show cpu", "#", 2*time.Second); err != nil {
			utils.Logline("error proccesing 'show cpu'", host.Ip.String(), err)
			errChan <- err
			return
		}
		response = strings.TrimSpace(utils.OltRemoveLastLine(response))
		for _, line := range strings.Split(response, "\n") {
			if strings.Contains(line, "1min") {
				response = strings.ReplaceAll(line, "load average(1min)", "")
				response = strings.TrimSpace(strings.ReplaceAll(response, ":", ""))

				item := findHostItemSn(items, host.Id, ".1.9.1.1.1") //use oid of zte olt
				if item != nil {
					itemId = item.ItemId
				} else {
					itemId = createItemSnmp(db, host.Id, host.Ip, ".1.9.1.1.1", "olt-card-cpuload-1")
				}

				if itemId != "" && response != "" {
					result = append(result, models.ItemResult{ItemId: itemId, Value: response, Table: "detalle_int"})
				}
			}
		}

		//send command and read response - get fan
		itemId = ""
		if response, err = utils.OltZteSend(connTelnet, "show fan", "#", 2*time.Second); err != nil {
			utils.Logline("error proccesing 'show cpu'", host.Ip.String(), err)
			errChan <- err
			return
		}
		response = strings.TrimSpace(utils.OltRemoveLastLine(response))
		for _, line := range strings.Split(response, "\n") {
			i := 1
			if strings.Contains(line, "status") {
				re := regexp.MustCompile(`\((\d+)rpm\)`)
				match := re.FindStringSubmatch(line)
				if len(match) > 1 {
					snItem := fmt.Sprintf(".7.1.1.%d", i) //use oid of zte olt
					nombreItem := fmt.Sprintf("olt-fan-%d", i)
					item := findHostItemSn(items, host.Id, snItem)
					if item != nil {
						itemId = item.ItemId
					} else {
						itemId = createItemSnmp(db, host.Id, host.Ip, snItem, nombreItem)
					}

					if itemId != "" && response != "" {
						result = append(result, models.ItemResult{ItemId: itemId, Value: match[1], Table: "detalle_int"})
					}
				}
				i++
			}
		}

		connTelnet.Close()

		//connect to snmp
		connSnmp, err := utils.OltSnmpConnect(host.Ip.String(), host.SnmpCommunity, 4, 4, true)
		if err != nil {
			utils.Logline("Couldnt establish connection", err)
			errChan <- err
			return
		}
		defer connSnmp.Conn.Close()

		//get modelo and uptime via snmp
		oids := []string{
			".1.3.6.1.4.1.17409.2.3.1.2.1.1.3.1", //modelo
			".1.3.6.1.4.1.17409.2.3.1.2.1.1.5.1", //uptime
		}
		resultSnmp, err := connSnmp.Get(oids)
		if err != nil {
			utils.Logline("Error performing Get for OIDs", host.Ip.String(), oids, err)
			errChan <- err
			return
		}
		for _, value := range resultSnmp.Variables {
			itemId = ""
			var itemValue, itemTable string
			oid := value.Name

			//change the oid to use in DB the same as the OLT ZTEs
			if oid == ".1.3.6.1.4.1.17409.2.3.1.2.1.1.3.1" {
				oid = ".1.3.6.1.2.1.1.1.0"
			} else {
				oid = ".1.3.6.1.2.1.1.3.0"
			}

			item := findHostItemSn(items, host.Id, oid)
			if item != nil {
				itemId = item.ItemId
			}

			switch oid {
			case ".1.3.6.1.2.1.1.1.0":
				modelo := "cdata " + strings.ToLower(fmt.Sprintf("%s", value.Value))
				if len(itemId) <= 1 {
					itemId = createItemSnmp(db, host.Id, host.Ip, oid, "olt-devmodel")
				}
				itemValue = modelo
				itemTable = "detalle_text"
			case ".1.3.6.1.2.1.1.3.0":
				uptime := fmt.Sprintf("%d", value.Value)
				if len(itemId) <= 1 {
					itemId = createItemSnmp(db, host.Id, host.Ip, oid, "olt-uptime")
				}
				itemValue = uptime
				itemTable = "detalle_int"
			}

			if itemValue != "" && itemId != "" {
				result = append(result, models.ItemResult{ItemId: itemId, Value: itemValue, Table: itemTable})
			}
		}

		connSnmp.Conn.Close()

		resultChan <- result

	}()

	//wait for response on errChan or resultChan
	select {
	case <-ctx.Done():
		// Timeout occurred
		utils.Logline("timeout occurred while processing snmp on", host.Ip.String(), ctx.Err())
		return
	case err := <-errChan:
		if err != nil {
			utils.Logline("error processing snmp on", host.Ip.String(), err)
			return
		}
	case response := <-resultChan:
		if err := insertEstadistica(db, response, host.Ip.String(), "get_olt_info"); err != nil {
			utils.Logline("error inserting data", host.Ip.String(), err)
			return
		}
	}
}

func workerVsolInfo(wg *sync.WaitGroup, db models.ConnDb, host models.HostInfo, items []hostItems) {
	defer wg.Done()

	defer func() {
		// recover from panic if one occured. Set err to nil otherwise.
		if recover() != nil {
			utils.Logline("error on this subprocess - workerVsolInfo", host.Ip.String())
			return
		}
	}()

	// Create a context with a timeout
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	//crear canal para recibir la respuesta de las operaciones en snmp y telnet
	errChan := make(chan error, 1)
	resultChan := make(chan []models.ItemResult, 1)

	go func() {
		var itemId, response string
		var err error
		var result []models.ItemResult
		// Connect to the OLT via telnet to get temp, cpu and fan
		connTelnet, err := utils.OltVsolConnect(host.Ip.String(), "23", host.TelnetUsername, host.TelnetPasswd)
		if err != nil {
			utils.Logline("Couldnt establish connection", err)
			errChan <- err
			return
		}
		defer connTelnet.Close()

		//send command and read response - get temperature
		itemId = ""
		if response, err = utils.OltZteSend(connTelnet, "show fan", "#", 2*time.Second); err != nil {
			utils.Logline("error proccesing 'show fan'", host.Ip.String(), err)
			errChan <- err
			return
		}
		response = strings.TrimSpace(utils.OltRemoveLastLine(response))
		for _, line := range strings.Split(response, "\n") {
			if strings.Contains(line, "current temperature") {
				response = strings.ReplaceAll(line, "current temperature:", "")
				response = strings.TrimSpace(strings.ReplaceAll(response, ".c", ""))
				item := findHostItemSn(items, host.Id, ".1.3.6.1.4.1.3902.1082.10.10.2.1.5.1.3.1.1") //use oid of zte olt
				if item != nil {
					itemId = item.ItemId
				} else {
					itemId = createItemSnmp(db, host.Id, host.Ip, ".1.3.6.1.4.1.3902.1082.10.10.2.1.5.1.3.1.1", "olt-temperature")
				}

				if itemId != "" && response != "" {
					result = append(result, models.ItemResult{ItemId: itemId, Value: response, Table: "detalle_int"})
				}
			}
		}

		connTelnet.Close()

		//connect to snmp
		connSnmp, err := utils.OltSnmpConnect(host.Ip.String(), host.SnmpCommunity, 4, 4, true)
		if err != nil {
			utils.Logline("Couldnt establish connection", err)
			errChan <- err
			return
		}
		defer connSnmp.Conn.Close()

		//get modelo and uptime via snmp
		oids := []string{
			".1.3.6.1.2.1.1.1.0",                 //modelo
			".1.3.6.1.4.1.37950.1.1.5.10.12.3.0", //cpu
			".1.3.6.1.2.1.1.3.0",                 //uptime
		}
		resultSnmp, err := connSnmp.Get(oids)
		if err != nil {
			utils.Logline("Error performing Get for OIDs", host.Ip.String(), oids, err)
			errChan <- err
			return
		}
		for _, value := range resultSnmp.Variables {
			itemId = ""
			var itemValue, itemTable string
			oid := value.Name

			//change the oid to use in DB the same as the OLT ZTEs
			if oid == ".1.3.6.1.4.1.37950.1.1.5.10.12.3.0" {
				oid = ".1.9.1.1.1"
			}

			item := findHostItemSn(items, host.Id, oid)
			if item != nil {
				itemId = item.ItemId
			}

			switch oid {
			case ".1.3.6.1.2.1.1.1.0":
				modelo := "vsol " + strings.ToLower(fmt.Sprintf("%s", value.Value))
				if len(itemId) <= 1 {
					itemId = createItemSnmp(db, host.Id, host.Ip, oid, "olt-devmodel")
				}
				itemValue = modelo
				itemTable = "detalle_text"
			case ".1.9.1.1.1":
				cpu := fmt.Sprintf("%d", value.Value)
				if len(itemId) <= 1 {
					itemId = createItemSnmp(db, host.Id, host.Ip, oid, "olt-card-cpuload-1")
				}
				itemValue = cpu
				itemTable = "detalle_int"
			case ".1.3.6.1.2.1.1.3.0":
				uptime := fmt.Sprintf("%d", value.Value)
				if len(itemId) <= 1 {
					itemId = createItemSnmp(db, host.Id, host.Ip, oid, "olt-uptime")
				}
				itemValue = uptime
				itemTable = "detalle_int"
			}

			if itemValue != "" && itemId != "" {
				result = append(result, models.ItemResult{ItemId: itemId, Value: itemValue, Table: itemTable})
			}
		}

		connSnmp.Conn.Close()

		resultChan <- result

	}()

	//wait for response on errChan or resultChan
	select {
	case <-ctx.Done():
		// Timeout occurred
		utils.Logline("timeout occurred while processing snmp on", host.Ip.String(), ctx.Err())
		return
	case err := <-errChan:
		if err != nil {
			utils.Logline("error processing snmp on", host.Ip.String(), err)
			return
		}
	case response := <-resultChan:
		if err := insertEstadistica(db, response, host.Ip.String(), "get_olt_info"); err != nil {
			utils.Logline("error inserting data", host.Ip.String(), err)
			return
		}
	}
}

// createItem of SNMP type
func createItemSnmp(db models.ConnDb, hostId string, hostIp netip.Addr, itemSn string, itemName string) (itemId string) {
	query := `WITH ins AS (
			INSERT INTO network.host_item (empresa_id, activo, host_id, sn, ip, nombre)
			SELECT 1, true, $1, $2::text, $3, $4
			WHERE NOT EXISTS (SELECT id FROM network.host_item WHERE host_id = $1 AND sn=$2::text)
			RETURNING id
		), upd as (
			UPDATE network.host_item SET activo=true, ip=$3, nombre=$4
			WHERE host_id = $1 AND sn=$2::text
			RETURNING id
		)
		SELECT id FROM ins
		UNION ALL
		SELECT id FROM upd
		UNION ALL
		SELECT id FROM network.host_item WHERE host_id = $1 AND sn=$2::text
		LIMIT 1`

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := db.Conn.QueryRow(ctx, query, hostId, itemSn, hostIp, itemName).Scan(&itemId); err != nil {
		utils.Logline("error inserting network.host_item ("+itemName+")", hostId, hostIp.String(), err)
		return ""
	}

	return itemId
}

// funcion para buscar en un arreglo de tipo hostItems el itemId por Nombre
func findHostItemSn(hosts []hostItems, targetHostId string, targetSn string) *hostItems {
	for _, item := range hosts {
		if item.ItemSn == targetSn && item.HostId == targetHostId {
			return &item
		}
	}
	return nil
}

// funcion para insertar data a DB
func insertEstadistica(db models.ConnDb, results []models.ItemResult, host string, workerInfo string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	//open a transaction
	tx, err := db.Conn.Begin(ctx)
	if err != nil {
		utils.Logline("error starting transaction", host, err)
		return err
	}
	defer func() {
		if err != nil {
			tx.Rollback(ctx) // Rollback if there's an error
			utils.Logline("transaction rolled back due to error", host, "getOltInfo", err)
		}
	}()

	//process results chan
	var countInt, countTxt int
	for _, item := range results {
		if item.Table == "detalle_int" {
			queryInternal := "INSERT INTO estadistica.detalle_int(item_id, value) VALUES ($1, $2)"
			_, err = tx.Exec(ctx, queryInternal, item.ItemId, item.Value)
			if err != nil {
				return err
			}
			countInt++
		} else {
			queryInternal := "INSERT INTO estadistica.detalle_text(item_id, value) VALUES ($1, $2)"
			_, err = tx.Exec(ctx, queryInternal, item.ItemId, item.Value)
			if err != nil {
				return err
			}
			countTxt++
		}
	}

	//commit transaction
	err = tx.Commit(ctx)
	if err != nil {
		return err
	}

	utils.Logline(fmt.Sprintf("(%d) inserts on detalle_text - (%d) inserts on detalle_int", countTxt, countInt), workerInfo, host)

	return nil
}

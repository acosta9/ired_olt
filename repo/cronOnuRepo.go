package repo

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"ired.com/olt/models"
	"ired.com/olt/utils"
)

type itemsCronOnu struct {
	itemId     string
	itemSn     sql.NullString
	itemOldId  sql.NullString
	itemNombre sql.NullString
	itemActivo bool
}

func CronOnuInfo(db models.ConnDb, caller string) error {
	//show status of worker
	utils.Logline(utils.ShowStatusWorker(db, "onuInfo", caller+"/begin"))

	//get olts to work on
	query := `SELECT h.id, h.ip, h.nombre, h.info->>'telnet_username' as username, h.info->>'telnet_password' as password, h.info->>'snmp_read_community' as community
		FROM network.host as h
		WHERE h.info->>'telnet_username' IS NOT NULL AND h.info->>'snmp_read_community' IS NOT NULL AND h.activo=true
		ORDER BY RANDOM()`
	rows, err := db.Conn.Query(db.Ctx, query)
	if err != nil {
		utils.Logline("error getting host to run cron", err)
		return err
	}
	defer rows.Close()

	//create slice of hosts
	var hostsInfo []models.HostInfo
	for rows.Next() {
		var host models.HostInfo
		err = rows.Scan(&host.Id, &host.Ip, &host.Name, &host.TelnetUsername, &host.TelnetPasswd, &host.SnmpCommunity)
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
			if host.TelnetUsername == "vsol" {
				utils.Logline("vsol worker on construction", host.Ip.String())
				wg.Done()
			} else if host.TelnetUsername == "cdata" {
				utils.Logline("cdata worker on construction", host.Ip.String())
				wg.Done()
			} else {
				workerZteOnuInfo(&wg, db, host)
			}
		}()
	}

	wg.Wait()

	//show status of worker
	utils.Logline(utils.ShowStatusWorker(db, "onuInfo", caller+"/ending"))

	return nil
}

func workerZteOnuInfo(wg *sync.WaitGroup, db models.ConnDb, host models.HostInfo) {
	defer wg.Done()

	defer func() {
		// recover from panic if one occured. Set err to nil otherwise.
		if recover() != nil {
			utils.Logline("error on this subprocess - workerOnusInfoZte", host.Ip.String())
			return
		}
	}()

	// Create a context with a timeout
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()

	//crear canal para recibir la respuesta de las operaciones en snmp y telnet
	errChan := make(chan error, 1)

	go func() {
		var queryInternal, oid string
		var cont, totalItems int

		//getItems onu from DB
		items, err := getOnusInfoItems(db, host.Id)
		if err != nil {
			utils.Logline("error getting host - items from olts", host.Ip.String(), host.Name, err)
			errChan <- err
			return
		}

		//connect to snmp
		connSnmp, err := utils.OltSnmpConnect(host.Ip.String(), host.SnmpCommunity, 10, 10, true)
		if err != nil {
			utils.Logline("Couldnt establish connection", host.Name, err)
			errChan <- err
			return
		}
		defer connSnmp.Conn.Close()

		//create context
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		//open a transaction
		tx1, err := db.Conn.Begin(ctx)
		if err != nil {
			utils.Logline("error starting transaction", host.Ip.String(), host.Name, err)
			errChan <- err
			return
		}
		defer func() {
			if err != nil {
				tx1.Rollback(ctx) // Rollback if there's an error
				utils.Logline("transaction rolled back due to error", host.Ip.String(), host.Name, "getZteOnuInfo", err)
			}
		}()

		//get onus Names
		oid = ".1.3.6.1.4.1.3902.1082.500.10.2.3.3.1.2"
		resultSnmp, err := connSnmp.BulkWalkAll(oid)
		if err != nil {
			// connSnmp.Conn.Close() // close snmp connection if there's an error
			utils.Logline("Error performing BulkWalk: ", host.Ip.String(), host.Name, oid, err)
			errChan <- err
			return
		}
		for _, value := range resultSnmp {
			totalItems++
			snmpOid := value.Name
			snmpIndex := strings.ReplaceAll(snmpOid, ".1.3.6.1.4.1.3902.1082.500.10.2.3.3.1.2.", "")
			snmpOnuName := fmt.Sprintf("%s", value.Value)
			onuOldId, err := oldIdFromOnuName(snmpOnuName)
			if err != nil {
				utils.Logline("error extracting oldId", host.Ip.String(), host.Name, err)
				continue
			}

			//check if itemSn exist and if exist if oldid from DB is the same from olt
			if onuItemDb := findOnuBy(items, "itemSn", snmpIndex); onuItemDb == nil && len(onuOldId) > 2 {
				cont++
				utils.Logline(host.Name, fmt.Sprintf(`INSERT INTO network.host_item (empresa_id, host_id, nombre, sn, oldid) VALUES (1, %s, 'onu-sn', %s, %s)`, host.Id, snmpIndex, onuOldId))

				queryInternal = `INSERT INTO network.host_item (empresa_id, host_id, nombre, sn, oldid) VALUES (1, $1, 'onu-sn', $2, $3)`
				if _, err := tx1.Exec(ctx, queryInternal, host.Id, snmpIndex, onuOldId); err != nil {
					utils.Logline("error inserting network.host_item", host.Ip.String(), host.Name, err)
					tx1.Rollback(ctx)
					errChan <- err
					return
				}
			} else if onuItemDb.itemOldId.String != onuOldId && len(onuOldId) > 2 {
				cont++
				utils.Logline(host.Name, fmt.Sprintf(`UPDATE network.host_item SET activo=True, oldid=%s WHERE host_id=%s AND sn=%s`, onuOldId, host.Id, snmpIndex))

				queryInternal = `UPDATE network.host_item SET activo=True, oldid=$1 WHERE host_id=$2 AND sn=$3`
				if _, err := tx1.Exec(ctx, queryInternal, onuOldId, host.Id, snmpIndex); err != nil {
					utils.Logline("error updating network.host_item", host.Ip.String(), host.Name, err)
					tx1.Rollback(ctx)
					errChan <- err
					return
				}
			} else if !onuItemDb.itemActivo && len(onuOldId) > 2 {
				cont++
				utils.Logline(host.Name, fmt.Sprintf(`UPDATE network.host_item SET activo=True WHERE host_id=%s AND sn=%s`, host.Id, snmpIndex))

				queryInternal = `UPDATE network.host_item SET activo=True WHERE host_id=$1 AND sn=$2`
				if _, err := tx1.Exec(ctx, queryInternal, host.Id, snmpIndex); err != nil {
					utils.Logline("error updating network.host_item", host.Ip.String(), host.Name, err)
					tx1.Rollback(ctx)
					errChan <- err
					return
				}
			}

			//check if itemEthlist exist
			if onuItemDb := findOnuBy(items, "itemEthlist", snmpIndex); onuItemDb == nil && len(onuOldId) > 2 {
				cont++
				utils.Logline(host.Name, fmt.Sprintf(`INSERT INTO network.host_item (empresa_id, host_id, nombre, sn, oldid) VALUES (1, %s, 'onu-ethlist', %s, %s)`, host.Id, snmpIndex, onuOldId))

				queryInternal = `INSERT INTO network.host_item (empresa_id, host_id, nombre, sn, oldid) VALUES (1, $1, 'onu-ethlist', $2, $3)`
				if _, err := tx1.Exec(ctx, queryInternal, host.Id, snmpIndex, onuOldId); err != nil {
					utils.Logline("error inserting network.host_item", host.Ip.String(), host.Name, err)
					tx1.Rollback(ctx)
					errChan <- err
					return
				}
			}

			//check if itemOnuName exist
			if onuItemDb := findOnuBy(items, "itemOnuName", snmpIndex); onuItemDb == nil && len(onuOldId) > 2 {
				cont++
				utils.Logline(host.Name, fmt.Sprintf(`INSERT INTO network.host_item (empresa_id, host_id, nombre, sn, oldid) VALUES (1, %s, 'onu-name', %s, %s)`, host.Id, snmpIndex, onuOldId))

				queryInternal = `INSERT INTO network.host_item (empresa_id, host_id, nombre, sn, oldid) VALUES (1, $1, 'onu-name', $2, $3)`
				if _, err := tx1.Exec(ctx, queryInternal, host.Id, snmpIndex, onuOldId); err != nil {
					utils.Logline("error inserting network.host_item", host.Ip.String(), host.Name, err)
					tx1.Rollback(ctx)
					errChan <- err
					return
				}
			}

			//check if item onuTx exist
			if onuItemDb := findOnuBy(items, "itemOnuTx", snmpIndex); onuItemDb == nil && len(onuOldId) > 2 {
				cont++
				utils.Logline(host.Name, fmt.Sprintf(`INSERT INTO network.host_item (empresa_id, host_id, nombre, sn, oldid) VALUES (1, %s, 'onu-tx', %s, %s)`, host.Id, snmpIndex, onuOldId))

				queryInternal = `INSERT INTO network.host_item (empresa_id, host_id, nombre, sn, oldid) VALUES (1, $1, 'onu-tx', $2, $3)`
				if _, err := tx1.Exec(ctx, queryInternal, host.Id, snmpIndex, onuOldId); err != nil {
					tx1.Rollback(ctx)
					utils.Logline("error inserting network.host_item", host.Ip.String(), host.Name, err)
					errChan <- err
					return
				}
			}

			//check if item onuRx exist
			if onuItemDb := findOnuBy(items, "itemOnuRx", snmpIndex); onuItemDb == nil && len(onuOldId) > 2 {
				cont++
				utils.Logline(host.Name, fmt.Sprintf(`INSERT INTO network.host_item (empresa_id, host_id, nombre, sn, oldid) VALUES (1, %s, 'onu-rx', %s, %s)`, host.Id, snmpIndex, onuOldId))

				queryInternal = `INSERT INTO network.host_item (empresa_id, host_id, nombre, sn, oldid) VALUES (1, $1, 'onu-rx', $2, $3)`
				if _, err := tx1.Exec(ctx, queryInternal, host.Id, snmpIndex, onuOldId); err != nil {
					tx1.Rollback(ctx)
					utils.Logline("error inserting network.host_item", host.Ip.String(), host.Name, err)
					errChan <- err
					return
				}
			}

			//check if item onuStatus exist
			if onuItemDb := findOnuBy(items, "itemOnuStatus", snmpIndex); onuItemDb == nil && len(onuOldId) > 2 {
				cont++
				utils.Logline(host.Name, fmt.Sprintf(`INSERT INTO network.host_item (empresa_id, host_id, nombre, sn, oldid) VALUES (1, %s, 'onu-status', %s, %s)`, host.Id, snmpIndex, onuOldId))

				queryInternal = `INSERT INTO network.host_item (empresa_id, host_id, nombre, sn, oldid) VALUES (1, $1, 'onu-status', $2, $3)`
				if _, err := tx1.Exec(ctx, queryInternal, host.Id, snmpIndex, onuOldId); err != nil {
					tx1.Rollback(ctx)
					utils.Logline("error inserting network.host_item", host.Ip.String(), host.Name, err)
					errChan <- err
					return
				}
			}
		}
		// connSnmp.Conn.Close()

		//commit transaction
		err = tx1.Commit(ctx)
		if err != nil {
			utils.Logline("error commiting transaction to insert and update host_items", host.Ip.String(), host.Name, err)
			errChan <- err
			return
		}

		//getItems onu again after inserting or updating from DB
		if len(queryInternal) > 30 {
			items, err = getOnusInfoItems(db, host.Id)
			if err != nil {
				utils.Logline("error getting updated hostItems", host.Ip.String(), host.Name, err)
				errChan <- err
				return
			}
			utils.Logline(host.Name, fmt.Sprintf("(%d) records inserted/updated on network.host_item", cont), host.Ip.String(), "get_onu_info")
		}

		// insert into db, onus-names every 30min
		if time.Now().Minute()%30 == 0 {
			var cont int

			//open a transaction
			tx2, err := db.Conn.Begin(ctx)
			if err != nil {
				utils.Logline("error starting transaction", host.Ip.String(), host.Name, err)
				errChan <- err
				return
			}
			defer func() {
				if err != nil {
					tx2.Rollback(ctx) // Rollback if there's an error
					utils.Logline("transaction rolled back due to error", host.Ip.String(), host.Name, "InsertZteOnuName", err)
				}
			}()

			for _, value := range resultSnmp {
				snmpIndex := strings.ReplaceAll(value.Name, ".1.3.6.1.4.1.3902.1082.500.10.2.3.3.1.2.", "")
				snmpValue := fmt.Sprintf("%s", value.Value)

				//get host_item.id and create sql for transaction
				if onuItemDb := findOnuBy(items, "itemOnuName", snmpIndex); onuItemDb != nil {
					cont++
					queryInternal = `INSERT INTO estadistica.detalle_text (item_id, value) VALUES ($1, $2)`
					if _, err := tx2.Exec(ctx, queryInternal, onuItemDb.itemId, snmpValue); err != nil {
						utils.Logline("error inserting estadistica.detalle_text", host.Ip.String(), host.Name, err)
						tx2.Rollback(ctx)
						errChan <- err
						return
					}
				}
			}

			//commit transaction
			err = tx2.Commit(ctx)
			if err != nil {
				errChan <- err
				return
			}

			utils.Logline(fmt.Sprintf("(%d) records inserted of onu-status and onu-name", cont), host.Ip.String(), host.Name, "get_onu_info", "zteOnusName")
		}

		var wgInternal sync.WaitGroup

		// insert into db, onus-status every time this cron runs
		wgInternal.Add(1)
		go func() {
			zteOnusStatus(&wgInternal, db, host, items, totalItems)
		}()

		// insert into db, onus-rx every 5min
		if (time.Now().Minute()-1)%5 == 0 {
			wgInternal.Add(1)
			go func() {
				zteOnusRx(&wgInternal, db, host, items, totalItems)
			}()
		}

		// insert into db, onus-sn every 30min
		if (time.Now().Minute()-2)%30 == 0 {
			wgInternal.Add(1)
			go func() {
				zteOnusSn(&wgInternal, db, host, items, totalItems)
			}()
		}

		wgInternal.Wait()
		errChan <- nil
	}()

	//wait for response on errChan or ctx
	select {
	case <-ctx.Done():
		// Timeout occurred
		utils.Logline("timeout occurred while processing snmp on", host.Ip.String(), host.Name, ctx.Err())
		return
	case err := <-errChan:
		if err != nil {
			utils.Logline("error processing snmp on", host.Ip.String(), host.Name, err)
			return
		}
	}
}

func zteOnusStatus(wg *sync.WaitGroup, db models.ConnDb, host models.HostInfo, items []itemsCronOnu, totalItems int) {
	defer wg.Done()

	//create context
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	//connect to snmp
	connSnmp, err := utils.OltSnmpConnect(host.Ip.String(), host.SnmpCommunity, 20, 20, false)
	if err != nil {
		utils.Logline("Couldnt establish connection", host.Ip.String(), host.Name, "zteOnusStatus", err)
		return
	}
	defer connSnmp.Conn.Close()

	//open a transaction
	tx, err := db.Conn.Begin(ctx)
	if err != nil {
		utils.Logline("error starting transaction", host.Ip.String(), host.Name, "zteOnusStatus", err)
		return
	}
	defer func() {
		if err != nil {
			tx.Rollback(ctx) // Rollback if there's an error
			utils.Logline("transaction rolled back due to error", host.Name, "getZteOnuStatus", err)
		}
	}()

	//save onu-status every time this cron runs
	var cont int
	oid := ".1.3.6.1.4.1.3902.1082.500.10.2.3.8.1.4" // get onus status
	itemsRetrieved := 0
	for itemsRetrieved < totalItems {
		select {
		case <-ctx.Done():
			utils.Logline(fmt.Sprintf("timeout occurred while processing snmp zteOnusStatus itemsTotal (%d)", itemsRetrieved), host.Ip.String(), host.Name, ctx.Err())
			tx.Rollback(ctx)
			return
		default:
			// Continue with the operation
		}

		result, err := connSnmp.GetBulk([]string{oid}, 0, connSnmp.MaxRepetitions)
		if err != nil {
			// connSnmp.Conn.Close() // close snmp connection if there's an error
			utils.Logline("Error performing BulkWalk: ", host.Ip.String(), host.Name, "zteOnusStatus", oid, err)

			tx.Rollback(ctx) // Rollback if there's an error
			utils.Logline("transaction rolled back due to error", host.Name, "getZteOnuStatus", err)
			return
		}

		for _, value := range result.Variables {
			// fmt.Printf("OID: %s, Type: %s, Value: %v\n", value.Name, value.Type, value.Value)
			if strings.Contains(value.Name, ".1.3.6.1.4.1.3902.1082.500.10.2.3.8.1.4.") {
				snmpIndex := strings.ReplaceAll(value.Name, ".1.3.6.1.4.1.3902.1082.500.10.2.3.8.1.4.", "")
				snmpValue := fmt.Sprintf("%d", value.Value)

				//get host_item.id and create sql for transaction
				if onuItemDb := findOnuBy(items, "itemOnuStatus", snmpIndex); onuItemDb != nil {
					cont++
					queryInternal := `INSERT INTO estadistica.detalle_int (item_id, value) VALUES ($1, $2)`
					if _, err := tx.Exec(ctx, queryInternal, onuItemDb.itemId, snmpValue); err != nil {
						utils.Logline("error inserting estadistica.detalle_int", host.Ip.String(), host.Name, "zteOnusStatus", err)
						tx.Rollback(ctx)
						return
					}
				}
			}
			itemsRetrieved++
			oid = value.Name // Update OID for the next GetBulk request
		}

		if uint32(len(result.Variables)) < connSnmp.MaxRepetitions || !strings.Contains(oid, ".1.3.6.1.4.1.3902.1082.500.10.2.3.8.1.4.") {
			break // No more items to retrieve
		}
	}

	// commit transaction
	err = tx.Commit(ctx)
	if err != nil {
		utils.Logline("error executing the transaction", host.Ip.String(), host.Name, "zteOnusStatus", err)
		return
	}

	utils.Logline(fmt.Sprintf("(%d) records inserted of onu-status", cont), host.Ip.String(), host.Name, "get_onu_info", "zteOnusStatus")
}

func zteOnusRx(wg *sync.WaitGroup, db models.ConnDb, host models.HostInfo, items []itemsCronOnu, totalItems int) {
	defer wg.Done()

	//create context
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	//connect to snmp
	connSnmp, err := utils.OltSnmpConnect(host.Ip.String(), host.SnmpCommunity, 20, 20, false)
	if err != nil {
		utils.Logline("Couldnt establish connection", host.Ip.String(), host.Name, "zteOnusRx", err)
		return
	}
	defer connSnmp.Conn.Close()

	//open a transaction
	tx, err := db.Conn.Begin(ctx)
	if err != nil {
		utils.Logline("error starting transaction", host.Ip.String(), host.Name, "zteOnusRx", err)
		return
	}
	defer func() {
		if err != nil {
			tx.Rollback(ctx) // Rollback if there's an error
			utils.Logline("transaction rolled back due to error", host.Ip.String(), host.Name, "getZteOnuRx", err)
		}
	}()

	var cont int
	oid := ".1.3.6.1.4.1.3902.1082.500.1.2.4.2.1.2" // get onus rxs
	itemsRetrieved := 0
	for itemsRetrieved < totalItems {
		select {
		case <-ctx.Done():
			utils.Logline(fmt.Sprintf("timeout occurred while processing snmp getZteOnuRx itemsTotal (%d)", itemsRetrieved), host.Ip.String(), host.Name, ctx.Err())
			tx.Rollback(ctx)
			return
		default:
			// Continue with the operation
		}

		result, err := connSnmp.GetBulk([]string{oid}, 0, connSnmp.MaxRepetitions)
		if err != nil {
			// connSnmp.Conn.Close() // close snmp connection if there's an error
			utils.Logline("Error performing BulkWalk: ", host.Ip.String(), host.Name, "getZteOnuRx", oid, err)

			tx.Rollback(ctx) // Rollback if there's an error
			utils.Logline("transaction rolled back due to error", "getZteOnuRx", host.Name, err)
			return
		}

		for _, value := range result.Variables {
			// fmt.Printf("OID: %s, Type: %s, Value: %v\n", value.Name, value.Type, value.Value)
			if strings.Contains(value.Name, ".1.3.6.1.4.1.3902.1082.500.1.2.4.2.1.2.") {
				snmpIndex := strings.ReplaceAll(value.Name, ".1.3.6.1.4.1.3902.1082.500.1.2.4.2.1.2.", "")
				valueInt, err := strconv.ParseFloat(fmt.Sprintf("%d", value.Value), 32)
				if err != nil {
					utils.Logline("error converting rxValue to int", host.Ip.String(), host.Name, "zteOnusRx", value.Name, fmt.Sprintf("%d", value.Value), err)
					continue
				}

				//divir el numero entre 1000 ya que se recibe de snmp el valor sin separador de decimales
				snmpValue := "0"
				if valueInt != 0 {
					snmpValue = fmt.Sprintf("%.2f", valueInt/1000)
				}

				//get host_item.id and create sql for transaction
				if onuItemDb := findOnuBy(items, "itemOnuRx", snmpIndex); onuItemDb != nil {
					cont++
					queryInternal := `INSERT INTO estadistica.detalle_int (item_id, value) VALUES ($1, $2)`
					if _, err := tx.Exec(ctx, queryInternal, onuItemDb.itemId, snmpValue); err != nil {
						utils.Logline(host.Name, fmt.Sprintf(`INSERT INTO estadistica.detalle_int (item_id, value) VALUES (%s, %s)`, onuItemDb.itemId, snmpValue))
						utils.Logline("error inserting estadistica.detalle_int", host.Ip.String(), host.Name, "zteOnusRx", err)
						tx.Rollback(ctx)
						return
					}
				}
			}
			itemsRetrieved++
			oid = value.Name // Update OID for the next GetBulk request
		}

		if uint32(len(result.Variables)) < connSnmp.MaxRepetitions || !strings.Contains(oid, ".1.3.6.1.4.1.3902.1082.500.1.2.4.2.1.2.") {
			break // No more items to retrieve
		}
	}

	//commit transaction
	err = tx.Commit(ctx)
	if err != nil {
		utils.Logline("error executing the transaction", host.Ip.String(), host.Name, "zteOnusRx", err)
		return
	}

	utils.Logline(fmt.Sprintf("(%d) records inserted of onu-rx", cont), host.Ip.String(), host.Name, "get_onu_info", "zteOnusRx")

}

func zteOnusSn(wg *sync.WaitGroup, db models.ConnDb, host models.HostInfo, items []itemsCronOnu, totalItems int) {
	defer wg.Done()

	//create context
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	//connect to snmp
	connSnmp, err := utils.OltSnmpConnect(host.Ip.String(), host.SnmpCommunity, 20, 20, false)
	if err != nil {
		utils.Logline("Couldnt establish connection", host.Ip.String(), host.Name, "zteOnusSn", err)
		return
	}
	defer connSnmp.Conn.Close()

	//open a transaction
	tx, err := db.Conn.Begin(ctx)
	if err != nil {
		utils.Logline("error starting transaction", host.Ip.String(), host.Name, "zteOnusSn", err)
		return
	}
	defer func() {
		if err != nil {
			tx.Rollback(ctx) // Rollback if there's an error
			utils.Logline("transaction rolled back due to error", host.Ip.String(), host.Name, "getZteOnuSn", err)
		}
	}()

	var cont int
	oid := ".1.3.6.1.4.1.3902.1082.500.10.2.3.3.1.18" // get onus sns
	itemsRetrieved := 0
	for itemsRetrieved < totalItems {
		select {
		case <-ctx.Done():
			utils.Logline(fmt.Sprintf("timeout occurred while processing snmp zteOnusSn itemsTotal (%d)", itemsRetrieved), host.Ip.String(), host.Name, ctx.Err())
			tx.Rollback(ctx)
			return
		default:
			// Continue with the operation
		}

		result, err := connSnmp.GetBulk([]string{oid}, 0, connSnmp.MaxRepetitions)
		if err != nil {
			// connSnmp.Conn.Close() // close snmp connection if there's an error
			utils.Logline("Error performing BulkWalk: ", host.Ip.String(), "zteOnusSn", host.Name, oid, err)

			tx.Rollback(ctx) // Rollback if there's an error
			utils.Logline("transaction rolled back due to error", "zteOnusSn", host.Name, err)
			return
		}

		for _, value := range result.Variables {
			// fmt.Printf("OID: %s, Type: %s, Value: %v\n", value.Name, value.Type, value.Value)
			if strings.Contains(value.Name, ".1.3.6.1.4.1.3902.1082.500.10.2.3.3.1.18.") {
				snmpIndex := strings.ReplaceAll(value.Name, ".1.3.6.1.4.1.3902.1082.500.10.2.3.3.1.18.", "")
				snmpValue := fmt.Sprintf("%s", value.Value)

				//get host_item.id and create sql for transaction
				if onuItemDb := findOnuBy(items, "itemSn", snmpIndex); onuItemDb != nil {
					cont++
					queryInternal := `INSERT INTO estadistica.detalle_text (item_id, value) VALUES ($1, $2)`
					if _, err := tx.Exec(ctx, queryInternal, onuItemDb.itemId, snmpValue); err != nil {
						utils.Logline("error inserting estadistica.detalle_text", host.Ip.String(), host.Name, "zteOnusSn", err)
						tx.Rollback(ctx)
						return
					}
				}
			}
			itemsRetrieved++
			oid = value.Name // Update OID for the next GetBulk request
		}

		if uint32(len(result.Variables)) < connSnmp.MaxRepetitions || !strings.Contains(oid, ".1.3.6.1.4.1.3902.1082.500.10.2.3.3.1.18.") {
			break // No more items to retrieve
		}
	}

	//commit transaction
	err = tx.Commit(ctx)
	if err != nil {
		return
	}

	utils.Logline(fmt.Sprintf("(%d) records inserted of onu-sn", cont), host.Ip.String(), host.Name, "getOnuInfo", "zteOnusSn")

}

func getOnusInfoItems(db models.ConnDb, hostId string) ([]itemsCronOnu, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	query := `SELECT hi.id as id, hi.sn as index, hi.oldid, hi.nombre as nombre, hi.activo
		FROM network.host_item as hi
		LEFT JOIN network.host as h ON h.id=hi.host_id AND h.empresa_id=hi.empresa_id
		WHERE h.id=$1 AND hi.nombre LIKE 'onu%'
		ORDER BY hi.id ASC`

	rows, err := db.Conn.Query(ctx, query, hostId)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	//create slice of hosts
	var items []itemsCronOnu
	for rows.Next() {
		var item itemsCronOnu
		err = rows.Scan(&item.itemId, &item.itemSn, &item.itemOldId, &item.itemNombre, &item.itemActivo)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	rows.Close()

	return items, nil
}

func oldIdFromOnuName(onuName string) (string, error) {
	parts := strings.Split(onuName, "_-_")
	if len(parts) <= 1 {
		return "", fmt.Errorf("error parsing name: %s", onuName)
	}

	oldIdNum, err := strconv.Atoi(parts[len(parts)-1])
	if err != nil {
		return "", fmt.Errorf("error converting string to int: %s - %w", parts[len(parts)-1], err)
	}

	return fmt.Sprintf("%d", oldIdNum), nil
}

func findOnuBy(items []itemsCronOnu, targetItemName string, targetSnmpIndex string) *itemsCronOnu {
	switch targetItemName {
	case "itemSn":
		for _, item := range items {
			if item.itemSn.String == targetSnmpIndex && item.itemNombre.String == "onu-sn" {
				return &item
			}
		}
	case "itemEthlist":
		for _, item := range items {
			if item.itemSn.String == targetSnmpIndex && item.itemNombre.String == "onu-ethlist" {
				return &item
			}
		}
	case "itemOnuName":
		for _, item := range items {
			if item.itemSn.String == targetSnmpIndex && item.itemNombre.String == "onu-name" {
				return &item
			}
		}
	case "itemOnuTx":
		for _, item := range items {
			if item.itemSn.String == targetSnmpIndex && item.itemNombre.String == "onu-tx" {
				return &item
			}
		}
	case "itemOnuRx":
		for _, item := range items {
			if item.itemSn.String == targetSnmpIndex && item.itemNombre.String == "onu-rx" {
				return &item
			}
		}
	case "itemOnuStatus":
		for _, item := range items {
			if item.itemSn.String == targetSnmpIndex && item.itemNombre.String == "onu-status" {
				return &item
			}
		}
	}

	return nil
}

// onu-status		every 1min
// onu-rx				every 5min
// onu-name			every 30min
// onu-sn				every 30min
// onu-ethlist	sinc-manual
// onu-tx				sinc-manual

// ".1.3.6.1.4.1.3902.1082.500.10.2.3.8.1.4", //onusStatus
// 	// logging (1)
// 	// los (2)
// 	// syncMib (3)
// 	// working (4)
// 	// dyingGasp (5)
// 	// authFailed (6)
// 	// offline (7)

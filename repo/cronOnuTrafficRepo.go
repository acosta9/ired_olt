package repo

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"ired.com/olt/models"
	"ired.com/olt/utils"
)

type itemsTrafficOnu struct {
	onuSn       string
	snmpIndex   string
	RxOctetRate int64
	TxOctetRate int64
	RxPktRate   int64
	TxPktRate   int64
}

type itemsTrafficResult struct {
	snmpIndex string
	valueData string
	valueType string
}

func CronOnuTraffic(db models.ConnDb, caller string) error {
	//show status of worker
	utils.Logline(utils.ShowStatusWorker(db, "onuTraffic", caller+"/begin"))

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
				utils.Logline("vsol worker on construction", host.Ip.String(), host.Name)
				wg.Done()
			} else if host.TelnetUsername == "cdata" {
				utils.Logline("cdata worker on construction", host.Ip.String(), host.Name)
				wg.Done()
			} else {
				workerZteOnuTraffic(&wg, db, host)
			}
		}()
	}

	wg.Wait()

	//show status of worker
	utils.Logline(utils.ShowStatusWorker(db, "onuTraffic", caller+"/ending"))

	return nil
}

func workerZteOnuTraffic(wg *sync.WaitGroup, db models.ConnDb, host models.HostInfo) {
	defer wg.Done()

	defer func() {
		// recover from panic if one occured. Set err to nil otherwise.
		if recover() != nil {
			utils.Logline("error on this subprocess - workerOnusTrafficZte", host.Ip.String(), host.Name)
			return
		}
	}()

	// Create a context with a timeout
	ctx, cancel := context.WithTimeout(context.Background(), 57*time.Second)
	defer cancel()

	//crear canal para recibir la respuesta de las operaciones en snmp y telnet
	errChan := make(chan error, 1)

	go func() {
		var items []itemsTrafficOnu
		var totalItems int

		//connect to snmp
		connSnmp, err := utils.OltSnmpConnect(host.Ip.String(), host.SnmpCommunity, 20, 20, true)
		if err != nil {
			utils.Logline("Couldnt establish connection", "OnuTraffic", host.Name, err)
			errChan <- err
			return
		}
		defer connSnmp.Conn.Close()

		//get onus SN
		oid := ".1.3.6.1.4.1.3902.1082.500.10.2.3.3.1.18"
		resultSnmp, err := connSnmp.BulkWalkAll(oid)
		if err != nil {
			utils.Logline("Error performing BulkWalk: ", host.Ip.String(), host.Name, "OnuTraffic", oid, err)
			errChan <- err
			return
		}
		for _, value := range resultSnmp {
			snmpOid := value.Name
			snmpIndex := strings.ReplaceAll(snmpOid, ".1.3.6.1.4.1.3902.1082.500.10.2.3.3.1.18.", "")
			snmpOnuSnPart := strings.Split(fmt.Sprintf("%s", value.Value), ",")
			snmpOnuSn := snmpOnuSnPart[len(snmpOnuSnPart)-1]

			if onu := findItemBySn(items, snmpIndex); onu == nil {
				items = append(items, itemsTrafficOnu{snmpIndex: snmpIndex, onuSn: snmpOnuSn, RxOctetRate: 0, TxOctetRate: 0, RxPktRate: 0, TxPktRate: 0})
				totalItems++
			}
		}
		connSnmp.Conn.Close()

		//crear canal para recibir la respuesta de las operaciones en snmp
		var wgInternal sync.WaitGroup
		results := make(chan itemsTrafficResult, 300)

		wgInternal.Add(1)
		go zteOnusBytes(&wgInternal, host, totalItems, results)
		wgInternal.Add(1)
		go zteOnusPkts(&wgInternal, host, totalItems, results)

		go func() {
			wgInternal.Wait()
			close(results)
		}()

		for result := range results {
			index := findItemBySn(items, result.snmpIndex)
			switch result.valueType {
			case "RxOctetRate":
				items[*index].RxOctetRate = utils.BytesToKb(result.valueData)
			case "TxOctetRate":
				items[*index].TxOctetRate = utils.BytesToKb(result.valueData)
			case "RxPktRate":
				items[*index].RxPktRate = utils.StringToInt64(result.valueData)
			case "TxPktRate":
				items[*index].TxPktRate = utils.StringToInt64(result.valueData)
			}
		}

		//open a transaction
		tx1, err := db.Conn.Begin(ctx)
		if err != nil {
			utils.Logline("error starting transaction", host.Ip.String(), host.Name, "OnuTraffic", err)
			errChan <- err
			return
		}
		defer func() {
			if err != nil {
				tx1.Rollback(ctx) // Rollback if there's an error
				utils.Logline("transaction rolled back due to error", host.Ip.String(), host.Name, "OnuTraffic", err)
			}
		}()

		var queryInternal string
		var cont int
		for _, item := range items {
			queryInternal = `INSERT INTO estadistica.traffic_onu (sn, kbup, kbdw, pkup, pkdw) VALUES ($1, $2, $3, $4, $5)`
			if _, err := tx1.Exec(ctx, queryInternal, item.onuSn, item.RxOctetRate, item.TxOctetRate, item.RxPktRate, item.TxPktRate); err != nil {
				utils.Logline("error inserting network.host_item", host.Ip.String(), host.Name, "OnuTraffic", err)
				tx1.Rollback(ctx)
				errChan <- err
				return
			}
			cont++
		}

		//commit transaction
		err = tx1.Commit(ctx)
		if err != nil {
			utils.Logline("error executing the transaction", host.Ip.String(), host.Name, "OnuTraffic", err)
			errChan <- err
			return
		}

		utils.Logline(fmt.Sprintf("(%d) records inserted", cont), host.Ip.String(), host.Name, "OnuTraffic")

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

func zteOnusBytes(wg *sync.WaitGroup, host models.HostInfo, totalItems int, results chan<- itemsTrafficResult) {
	defer wg.Done()

	// Create a context with a timeout
	ctx, cancel := context.WithTimeout(context.Background(), 51*time.Second)
	defer cancel()

	//connect to snmp
	connSnmp, err := utils.OltSnmpConnect(host.Ip.String(), host.SnmpCommunity, totalItems, 20, false)
	if err != nil {
		utils.Logline("Couldnt establish connection", host.Ip.String(), host.Name, "zteOnusBytes", err)
		return
	}
	defer connSnmp.Conn.Close()

	oids := []string{
		".1.3.6.1.4.1.3902.1082.500.4.2.2.2.1.3",  // zxAnPonOnuIfRxOctetRate
		".1.3.6.1.4.1.3902.1082.500.4.2.2.2.1.46", // zxAnPonOnuIfTxOctetRate
	}
	for _, oid := range oids {
		itemsRetrieved := 0
		for itemsRetrieved < totalItems {
			select {
			case <-ctx.Done():
				utils.Logline(fmt.Sprintf("timeout occurred while processing snmp zteOnusBytes itemsTotal (%d)", itemsRetrieved), host.Ip.String(), host.Name, ctx.Err())
				return
			default:
				// Continue with the operation
			}
			result, err := connSnmp.GetBulk([]string{oid}, 0, connSnmp.MaxRepetitions)
			if err != nil {
				// connSnmp.Conn.Close() // close snmp connection if there's an error
				utils.Logline("Error performing BulkWalk: ", host.Ip.String(), host.Name, "zteOnusBytes", oid, err)
				return
			}

			for _, value := range result.Variables {
				// fmt.Printf("OID: %s, Type: %s, Value: %v\n", value.Name, value.Type, value.Value)
				switch {
				case strings.Contains(value.Name, ".1.3.6.1.4.1.3902.1082.500.4.2.2.2.1.3."):
					results <- itemsTrafficResult{
						snmpIndex: strings.ReplaceAll(value.Name, ".1.3.6.1.4.1.3902.1082.500.4.2.2.2.1.3.", ""),
						valueData: fmt.Sprintf("%d", value.Value),
						valueType: "RxOctetRate",
					}
				case strings.Contains(value.Name, ".1.3.6.1.4.1.3902.1082.500.4.2.2.2.1.46."):
					results <- itemsTrafficResult{
						snmpIndex: strings.ReplaceAll(value.Name, ".1.3.6.1.4.1.3902.1082.500.4.2.2.2.1.46.", ""),
						valueData: fmt.Sprintf("%d", value.Value),
						valueType: "TxOctetRate",
					}
				}
				itemsRetrieved++
				oid = value.Name // Update OID for the next GetBulk request
			}

			if uint32(len(result.Variables)) < connSnmp.MaxRepetitions || (!strings.Contains(oid, "3902.1082.500.4.2.2.2.1.3.") && !strings.Contains(oid, "3902.1082.500.4.2.2.2.1.46.")) {
				break // No more items to retrieve
			}
		}
	}
}

func zteOnusPkts(wg *sync.WaitGroup, host models.HostInfo, totalItems int, results chan<- itemsTrafficResult) {
	defer wg.Done()

	// Create a context with a timeout
	ctx, cancel := context.WithTimeout(context.Background(), 51*time.Second)
	defer cancel()

	//connect to snmp
	connSnmp, err := utils.OltSnmpConnect(host.Ip.String(), host.SnmpCommunity, totalItems, 20, false)
	if err != nil {
		utils.Logline("Couldnt establish connection", host.Ip.String(), host.Name, "zteOnusPkts", err)
		return
	}
	defer connSnmp.Conn.Close()

	oids := []string{
		".1.3.6.1.4.1.3902.1082.500.4.2.2.2.1.4",  // zxAnPonOnuIfRxPktRate
		".1.3.6.1.4.1.3902.1082.500.4.2.2.2.1.47", // zxAnPonOnuIfTxPktRate
	}

	for _, oid := range oids {
		itemsRetrieved := 0
		for itemsRetrieved < totalItems {
			select {
			case <-ctx.Done():
				utils.Logline(fmt.Sprintf("timeout occurred while processing snmp zteOnusPkts itemsTotal (%d)", itemsRetrieved), host.Ip.String(), host.Name, ctx.Err())
				return
			default:
				// Continue with the operation
			}
			result, err := connSnmp.GetBulk([]string{oid}, 0, connSnmp.MaxRepetitions)
			if err != nil {
				// connSnmp.Conn.Close() // close snmp connection if there's an error
				utils.Logline("Error performing BulkWalk: ", host.Ip.String(), host.Name, "zteOnusPkts", oid, err)
				return
			}

			for _, value := range result.Variables {
				// fmt.Printf("OID: %s, Type: %s, Value: %v\n", value.Name, value.Type, value.Value)
				switch {
				case strings.Contains(value.Name, ".1.3.6.1.4.1.3902.1082.500.4.2.2.2.1.4."):
					results <- itemsTrafficResult{
						snmpIndex: strings.ReplaceAll(value.Name, ".1.3.6.1.4.1.3902.1082.500.4.2.2.2.1.4.", ""),
						valueData: fmt.Sprintf("%d", value.Value),
						valueType: "RxPktRate",
					}
				case strings.Contains(value.Name, ".1.3.6.1.4.1.3902.1082.500.4.2.2.2.1.47."):
					results <- itemsTrafficResult{
						snmpIndex: strings.ReplaceAll(value.Name, ".1.3.6.1.4.1.3902.1082.500.4.2.2.2.1.47.", ""),
						valueData: fmt.Sprintf("%d", value.Value),
						valueType: "TxPktRate",
					}
				}
				itemsRetrieved++
				oid = value.Name // Update OID for the next GetBulk request
			}

			if uint32(len(result.Variables)) < connSnmp.MaxRepetitions || (!strings.Contains(oid, "3902.1082.500.4.2.2.2.1.4.") && !strings.Contains(oid, "3902.1082.500.4.2.2.2.1.47.")) {
				break // No more items to retrieve
			}
		}
	}

	connSnmp.Conn.Close()
}

func findItemBySn(items []itemsTrafficOnu, targetSnmpIndex string) *int {
	for index, item := range items {
		if item.snmpIndex == targetSnmpIndex {
			return &index
		}
	}
	return nil
}

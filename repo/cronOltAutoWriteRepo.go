package repo

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"

	"ired.com/olt/models"
	"ired.com/olt/utils"
)

func OltAutoWrite(db models.ConnMysqlPgsql, caller string) error {
	//show status of worker
	utils.Logline(utils.ShowStatusWorkerMysql(db, "oltAutoWrite", caller+"/begin"))

	//get olts to work on
	query := `SELECT h.id, h.ip, h.info->>'telnet_username' as username, h.info->>'telnet_password' as password, h.info->>'snmp_read_community' as community
		FROM network.host as h
		WHERE h.info->>'telnet_username' IS NOT NULL AND h.info->>'snmp_read_community' IS NOT NULL AND h.activo=true
		ORDER BY RANDOM()`
	rows, err := db.ConnPgsql.Query(db.Ctx, query)
	if err != nil {
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
			if host.TelnetUsername != "vsol" && host.TelnetUsername != "cdata" {
				workerZteAutoWrite(&wg, db, host)
			} else {
				wg.Done()
			}
		}()
	}

	wg.Wait()

	//show status of worker
	utils.Logline(utils.ShowStatusWorkerMysql(db, "oltAutoWrite", caller+"/ending"))

	return nil
}

func workerZteAutoWrite(wg *sync.WaitGroup, db models.ConnMysqlPgsql, host models.HostInfo) {
	defer wg.Done()

	defer func() {
		// recover from panic if one occured. Set err to nil otherwise.
		if recover() != nil {
			utils.Logline("error on this subprocess - workerZteClock", host.Ip.String())
			return
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	// Prepare the query statement
	stmt, err := db.ConnMysql.PrepareContext(ctx, `SELECT COUNT(ou.id) as num, MAX(ou.docred_id) as docred_id
		FROM estaciones_onu as ou 
		LEFT JOIN doc_red as dr ON dr.id=ou.docred_id
		WHERE dr.ip=? AND ou.sync=0
		GROUP BY ou.sync, ou.docred_id
		LIMIT 1`)
	if err != nil {
		utils.Logline("error on prepare stmt sync onus from mysql", host.Ip.String(), err)
		return
	}
	defer stmt.Close()

	var cont, docred_id int
	err = stmt.QueryRow(host.Ip.String()).Scan(&cont, &docred_id)
	if err != nil {
		if err != sql.ErrNoRows {
			utils.Logline("error executing query sync onus from mysql", host.Ip.String(), err)
			return
		}
	}

	if cont <= 0 {
		utils.Logline("There is no new pending ONU to save conf in olt", host.Ip.String())
		return
	}

	// Connect to the OLT
	conn, err := utils.OltZteConnect(host.Ip.String(), "23", host.TelnetUsername, host.TelnetPasswd)
	if err != nil {
		utils.Logline("Couldnt establish connection", err)
		return
	}
	defer conn.Close()

	//send command and read response
	if _, err = utils.OltZteSend(conn, "write", "#", 55*time.Second); err != nil {
		utils.Logline("error proccesing 'write'", host.Ip.String(), err)
		return
	}
	conn.Close()

	ctx, cancel = context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	//updating sync onus on olt
	query := `UPDATE estaciones_onu SET sync=1 WHERE docred_id=?`
	_, err = db.ConnMysql.ExecContext(ctx, query, docred_id)
	if err != nil {
		utils.Logline("error updating sync onus on db:", host.Ip.String(), err)
		return
	}

	utils.Logline(fmt.Sprintf("Write successfully - There were (%d) pending ONUs to save conf in olt", cont), host.Ip.String())

}

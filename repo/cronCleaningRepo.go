package repo

import (
	"database/sql"
	"fmt"

	"ired.com/olt/models"
	"ired.com/olt/utils"
)

func CleanOltData(db models.ConnDb, caller string) error {
	//show status of worker
	utils.Logline(utils.ShowStatusWorker(db, "cleanOldData", caller+"/begin"))

	//update host nombres de olts que difieran del nombre en documentacion
	query := `UPDATE network.host as h
		SET nombre=q0.nombre
		FROM (
			SELECT h.id, doc.nombre
			FROM network.documentacion as doc
			LEFT JOIN network.host as h ON h.ip=doc.ip
			WHERE doc.nombre like '%olt%' AND doc.existencia_id IS NOT NULL AND doc.nombre<>h.nombre
		) as q0
		WHERE h.id=q0.id`
	commandTag, err := db.Conn.Exec(db.Ctx, query)
	if err != nil {
		utils.Logline("error updating host nombre de olts", err)
		return err
	}
	utils.Logline("Rows affected update host nombres de olts que difieran del nombre en documentacion", commandTag.RowsAffected())

	//show status of worker
	utils.Logline(utils.ShowStatusWorker(db, "cleanOldData", caller+"/ending"))

	return nil
}

func CleanOnuData(db models.ConnDb, caller string) error {
	//show status of worker
	utils.Logline(utils.ShowStatusWorker(db, "cleanOldData", caller+"/begin"))

	//delete host_item where nombre is onu-tx2
	query := `DELETE FROM network.host_item WHERE nombre like 'onu-tx2'`
	commandTag, err := db.Conn.Exec(db.Ctx, query)
	if err != nil {
		utils.Logline("error deleting onu-tx2", err)
		return err
	}
	utils.Logline("Rows affected delete host_item where nombre is onu-tx2", commandTag.RowsAffected())

	//check if items with name onu-(sn|name|rx|status) are duplicated, and remove the ones that dont have data in the last 12hours
	query = `SELECT hi.oldid, hi.nombre
		FROM network.host_item as hi
		WHERE hi.nombre SIMILAR TO 'onu-(sn|name|rx|status)' AND hi.oldid IS NOT NULL
		GROUP BY hi.oldid, hi.nombre
		HAVING COUNT(*)>1`
	rows1, err := db.Conn.Query(db.Ctx, query)
	if err != nil {
		utils.Logline("error getting host_items that are duplicated", err)
		return err
	}
	defer rows1.Close()

	for rows1.Next() {
		var oldId, itemName string
		err = rows1.Scan(&oldId, &itemName)
		if err != nil {
			utils.Logline("error scanning rows of items to delete", err)
			return err
		}
		if err := validItemOnu(db, oldId, itemName); err != nil {
			utils.Logline("error validating item", err)
			continue
		}
	}
	rows1.Close()

	//check if items with name onu-(ethlist|tx) are duplicated, and remove the ones that dont have data in the last 12hours
	query = `SELECT hi.oldid, hi.nombre
		FROM network.host_item as hi
		WHERE hi.nombre SIMILAR TO 'onu-(ethlist|tx)' AND hi.oldid IS NOT NULL
		GROUP BY hi.oldid, hi.nombre
		HAVING COUNT(*)>1`
	rows2, err := db.Conn.Query(db.Ctx, query)
	if err != nil {
		utils.Logline("error getting host_items that are duplicated", err)
		return err
	}
	defer rows2.Close()

	for rows2.Next() {
		var oldId, itemName string
		err = rows2.Scan(&oldId, &itemName)
		if err != nil {
			utils.Logline("error scanning rows of items to delete", err)
			return err
		}
		if err := validItemOnu2(db, oldId, itemName); err != nil {
			utils.Logline("error validating item", err)
			continue
		}
	}
	rows2.Close()

	//show status of worker
	utils.Logline(utils.ShowStatusWorker(db, "cleanOldData", caller+"/ending"))

	return nil
}

func validItemOnu(db models.ConnDb, oldId string, itemName string) error {
	//this query looks if the items have recieve some value in the last 12h and also checks if hostId is active in the last 24h
	query := `WITH items AS (
			SELECT id, host_id FROM network.host_item WHERE oldid=$1 AND nombre=$2 ORDER BY id ASC
		), host_status AS (
			SELECT q0.host_id, 
				CASE
					WHEN q0.value>0 THEN true
				ELSE false
				END AS host_valid
			FROM(
				SELECT hi.host_id, MAX(icmp.value) as value
				FROM network.host_item as hi
				LEFT JOIN estadistica.icmp as icmp ON icmp.item_id=hi.id
				WHERE hi.host_id IN (SELECT host_id FROM items) AND hi.nombre LIKE 'avg' AND icmp.created_at>=NOW()-INTERVAL'24h'
				GROUP BY hi.host_id
			) as q0
		), detalle_int AS(
			SELECT hi.id, MAX(di.value)::text as value
			FROM network.host_item as hi
			LEFT JOIN estadistica.detalle_int as di ON di.item_id=hi.id
			WHERE hi.id IN (SELECT id FROM items) AND di.created_at>=NOW()-INTERVAL'12h'
			GROUP BY hi.id
		), detalle_text AS(
			SELECT hi.id, MAX(dt.value)::text as value
			FROM network.host_item as hi
			LEFT JOIN estadistica.detalle_text as dt ON dt.item_id=hi.id
			WHERE hi.id IN (SELECT id FROM items) AND dt.created_at>=NOW()-INTERVAL'12h'
			GROUP BY hi.id
		)

		SELECT item.id, item.host_id, di.value as di, dt.value as dt,
			CASE 
				WHEN hs.host_id IS NULL THEN true
				ELSE hs.host_valid
			END as host_valid
		FROM items as item
		LEFT JOIN host_status as hs ON hs.host_id=item.host_id
		LEFT JOIN detalle_int as di ON di.id=item.id
		LEFT JOIN detalle_text as dt ON dt.id=item.id
		ORDER BY item.id ASC
	`
	rows1, err := db.Conn.Query(db.Ctx, query, oldId, itemName)
	if err != nil {
		return fmt.Errorf("error on query items validity with Oldid(%s) and ItenName(%s) validity: %w", oldId, itemName, err)
	}
	defer rows1.Close()

	for rows1.Next() {
		var itemId, hostId string
		var hostValid bool
		var di, dt sql.NullString
		err = rows1.Scan(&itemId, &hostId, &di, &dt, &hostValid)
		if err != nil {
			return fmt.Errorf("error scanning item with Oldid(%s) and ItenName(%s) validity: %w", oldId, itemName, err)
		}

		if !hostValid {
			utils.Logline(fmt.Sprintf("cant validate item (%s) right now, host (%s) is not active", itemId, hostId))
			continue
		}

		if !di.Valid && !dt.Valid {
			utils.Logline(fmt.Sprintf("borrar item_id (%s), oldid (%s) of nombre (%s)", itemId, oldId, itemName))
			queryInternal := `DELETE FROM network.host_item WHERE id=$1`
			_, err := db.Conn.Exec(db.Ctx, queryInternal, itemId)
			if err != nil {
				utils.Logline("error deleting item_id", err)
				return err
			}
			return nil
		}
	}
	rows1.Close()

	return nil
}

func validItemOnu2(db models.ConnDb, oldId string, itemName string) error {
	//this query looks if onu-status exist for that specific sn, keep in mind that onu-ethlist and onu-tx dont store value often
	query := `SELECT sn FROM network.host_item WHERE oldid=$1 GROUP BY sn HAVING COUNT(*)<=2`
	rows1, err := db.Conn.Query(db.Ctx, query, oldId)
	if err != nil {
		return fmt.Errorf("error on query items validity with Oldid(%s) and ItenName(%s) validity: %w", oldId, itemName, err)
	}
	defer rows1.Close()

	for rows1.Next() {
		var itemSn string
		err = rows1.Scan(&itemSn)
		if err != nil {
			return fmt.Errorf("error scanning item with Oldid(%s) and ItenName(%s) validity: %w", oldId, itemName, err)
		}

		if itemSn != "" {
			utils.Logline(fmt.Sprintf("borrar item_sn (%s), oldid (%s) of nombre (%s)", itemSn, oldId, itemName))
			queryInternal := `DELETE FROM network.host_item WHERE sn=$1 AND oldid=$2 AND nombre=$3`
			_, err := db.Conn.Exec(db.Ctx, queryInternal, itemSn, oldId, itemName)
			if err != nil {
				utils.Logline("error deleting item_id", err)
				return err
			}
			return nil
		}
	}
	rows1.Close()

	return nil
}

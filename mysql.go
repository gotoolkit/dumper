package main

import (
	"database/sql"
	"fmt"
	"log"
	"time"

	"github.com/satori/go.uuid"
)

const (
	StateError = iota - 1
	StateProc
	StateDone
)

type mysql struct {
	DB            *sql.DB
	ID            uuid.UUID
	ProdOutput    string
	StagingOutput string
}

// NewProdDumper initial prod dumper
func NewProdDumper(db *sql.DB) *mysql {
	return &mysql{DB: db}
}

// NewDumper initial dumper
func NewDumper(db *sql.DB) *mysql {
	uid := uuid.NewV4()
	return &mysql{DB: db, ID: uid}
}
func (d *mysql) Ping() error {
	return d.DB.Ping()
}

func (d *mysql) DuplicateDatabase(duplicatDB string) error {
	_, err := d.DB.Exec(fmt.Sprintf("CREATE DATABASE %s", duplicatDB))
	if err != nil {
		return err
	}
	return nil
}

func (d *mysql) UseDB(db string) error {
	_, err := d.DB.Exec(fmt.Sprintf("USE `%s`", db))
	if err != nil {
		return err
	}
	return nil
}
func (d *mysql) UpdateStatus(message string, state int) error {
	log.Println("update task status", message)

	stmt, err := d.DB.Prepare("INSERT INTO `ihm_task_status` (`task_id`, `message`, `status`, `created_at`) VALUES (?, ?, ?, ?)")
	if err != nil {
		return err
	}
	_, err = stmt.Exec(d.ID, message, state, time.Now().Unix())
	if err != nil {
		return err
	}
	return nil
}

func (d *mysql) Close() error {
	return d.DB.Close()
}

func (d *mysql) UpdateProdPath(output string) error {
	d.ProdOutput = output
	return nil
}

func (d *mysql) UpdateStagingPath(output string) error {
	d.StagingOutput = output
	return nil
}
func (d *mysql) ImportSource(prodSource, stagingSource string) error {
	_, err := d.DB.Exec(fmt.Sprintf("SOURCE /home/dumper/%s;", prodSource))
	if err != nil {
		return err
	}
	_, err = d.DB.Exec(fmt.Sprintf("SOURCE /home/dumper/%s;", stagingSource))
	if err != nil {
		return err
	}
	return nil
}

func (d *mysql) Ping123() error {
	return nil
}

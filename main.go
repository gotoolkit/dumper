package main

import (
	"database/sql"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os/exec"
	"time"

	"github.com/gin-gonic/gin"
	_ "github.com/go-sql-driver/mysql"
	"github.com/pkg/errors"
	"github.com/satori/go.uuid"
)

type RsyncConfig struct {
	SrcHost  string `json:"src_host"`
	SrcUser  string `json:"src_user"`
	SrcPath  string `json:"src_path"`
	DestPath string `json:"dest_path"`
	DestUser string `json:"dest_user"`
	DestHost string `json:"dest_host"`
	DestPort int `json:"dest_port"`
}

type StagingConfig struct {
	Host     string   `json:"host"`
	Port     int      `json:"port"`
	User     string   `json:"user"`
	Password string   `json:"password"`
	Database string   `json:"database"`
	Tables   []string `json:"tables"`
}

type ProdConfig struct {
	Host              string   `json:"host"`
	Port              int      `json:"port"`
	User              string   `json:"user"`
	Password          string   `json:"password"`
	Database          string   `json:"database"`
	DuplicateDatabase string   `json:"duplicate_database"`
	Tables            []string `json:"tables"`
}

type SSHConfig struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	User     string `json:"user"`
	Password string `json:"password"`
}

type Config struct {
	StagingConfig StagingConfig `json:"staging"`
	ProdConfig    ProdConfig    `json:"prod"`
	RsyncConfig   RsyncConfig   `json:"rsync"`
	SSHConfig     SSHConfig     `json:"ssh"`
	SkipRsync     bool          `json:"skip_rsync"`
	SkipSed       bool          `json:"skip_sed"`
	SkipCC        bool          `json:"skip_cc"`
	YMLString     string        `json:"config_json"`
}

var (
	Rsync     = "rsync"
	MysqlDump = "mysqldump"
	Sed       = "sed"
	Mysql     = "/usr/bin/mysql"
	SSH       = "ssh"
)

func main() {
	router := gin.Default()
	router.POST("/dumper", dumpData)
	router.Run(":8085")
}

func dumpData(c *gin.Context) {
	var cfg Config
	if c.Bind(&cfg) == nil {
		log.Println("Config: ", cfg)
	}
	uid, err := setup(cfg)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
	} else {
		c.JSON(http.StatusOK, gin.H{"message": "OK", "task_id": uid})
	}
}

func setup(cfg Config) (uuid.UUID, error) {
	dataSource := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s", cfg.StagingConfig.User, cfg.StagingConfig.Password, cfg.StagingConfig.Host, cfg.StagingConfig.Port, cfg.StagingConfig.Database)
	db, err := sql.Open("mysql", dataSource)

	if err != nil {
		return uuid.Nil, errors.Wrap(err, "connect mysql failed")
	}

	dumper := NewDumper(db)

	err = dumper.Ping()
	if err != nil {
		return uuid.Nil, errors.Wrap(err, "ping mysql failed")
	}

	go startTask(dumper, cfg)

	return dumper.ID, nil
}

func startTask(dumper *mysql, cfg Config) {
	defer dumper.Close()
	// start sync
	dumper.UpdateStatus(fmt.Sprintf("Start Sync uploads folder"), StateProc)

	if !cfg.SkipRsync {
		err := syncUploads(cfg)
		if err != nil {
			dumper.UpdateStatus(fmt.Sprintf("Sync uploads folder failed: %v", err), StateError)
			return
		}
	} else {
		dumper.UpdateStatus(fmt.Sprint("Skip Sync uploads folder"), StateProc)
	}

	// dumping staging db
	dumper.UpdateStatus(fmt.Sprintf("Start dumping staging mysql: %s/%s", cfg.StagingConfig.Host, cfg.StagingConfig.Database), StateProc)

	stagingOutput, err := dumpStagingMysql(cfg)
	if err != nil {
		dumper.UpdateStatus(fmt.Sprintf("dumping staging mysql failed: %v", err), StateError)
		return
	}
	dumper.UpdateStagingPath(stagingOutput)
	dumper.UpdateStatus(fmt.Sprintf("finish dumping staging mysql: %s", stagingOutput), StateProc)

	// dumping prod db
	dumper.UpdateStatus(fmt.Sprintf("Start dumping prod mysql: %s/%s", cfg.ProdConfig.Host, cfg.ProdConfig.Database), StateProc)
	prodOutput, err := dumpProdMysql(cfg)
	if err != nil {
		dumper.UpdateStatus(fmt.Sprintf("dumping prod mysql failed: %v", err), StateError)
		return
	}
	dumper.UpdateProdPath(prodOutput)
	dumper.UpdateStatus(fmt.Sprintf("finish dumping prod mysql: %s", prodOutput), StateProc)

	// duplicate prod db
	dumper.UpdateStatus(fmt.Sprintf("Start duplicate prod mysql database: %s/%s", cfg.ProdConfig.Host, cfg.ProdConfig.Database), StateProc)
	err = copyProdMysql(cfg, dumper.ProdOutput, dumper.StagingOutput)
	if err != nil {
		dumper.UpdateStatus(fmt.Sprintf("duplicate prod mysql database failed: %v", err), StateError)
		return
	}

	// insert to prod duplicate db
	dumper.UpdateStatus(fmt.Sprintf("Start insert prod mysql: %s/%s", cfg.ProdConfig.Host, cfg.ProdConfig.Database), StateProc)

	err = importMysql(cfg, prodOutput, stagingOutput)
	if err != nil {
		dumper.UpdateStatus(fmt.Sprintf("insert prod mysql failed: %v", err), StateError)
		return
	}

	// modify config
	dumper.UpdateStatus(fmt.Sprint("Start modify config"), StateProc)
	if !cfg.SkipSed {

		err = modifyConfig(cfg)
		if err != nil {
			dumper.UpdateStatus(fmt.Sprintf("Modify config failed: %v", err), StateError)
			return
		}
	} else {
		dumper.UpdateStatus(fmt.Sprint("Skip modify config "), StateProc)
	}

	// start cc

	dumper.UpdateStatus(fmt.Sprint("Start cc1"), StateProc)
	if !cfg.SkipCC {
		err = cc(1022, "root", "srv1.cc")
		if err != nil {
			dumper.UpdateStatus(fmt.Sprintf("cc1 failed: %v", err), StateError)
			return
		}
	} else {
		dumper.UpdateStatus(fmt.Sprint("Skip cc1"), StateProc)
	}

	dumper.UpdateStatus(fmt.Sprint("Start cc2"), StateProc)
	if !cfg.SkipCC {
		err = cc(1022, "root", "srv2.cc")
		if err != nil {
			dumper.UpdateStatus(fmt.Sprintf("cc2 failed: %v", err), StateError)
			return
		}
	} else {
		dumper.UpdateStatus(fmt.Sprint("Skip cc2"), StateProc)
	}

	dumper.UpdateStatus(fmt.Sprint("Start cc3"), StateProc)
	if !cfg.SkipCC {
		err = cc(1022, "root", "srv3.cc")
		if err != nil {
			dumper.UpdateStatus(fmt.Sprintf("cc3 failed: %v", err), StateError)
			return
		}
	} else {
		dumper.UpdateStatus(fmt.Sprint("Skip cc3"), StateProc)
	}

	dumper.UpdateStatus(fmt.Sprint("Task completed"), StateDone)
}

func syncUploads(cfg Config) error {
	options := syncOptions(cfg)
	cmd := exec.Command(Rsync, options...)
	log.Println(cmd.Args)
	data, err := cmd.CombinedOutput()
	if err != nil {
		log.Println(string(data))
		return err
	}
	return nil
}

func syncOptions(cfg Config) []string {
	var options []string
	src := fmt.Sprintf("%s", cfg.RsyncConfig.SrcPath)
	dest := fmt.Sprintf("%s:%s", "gfs", cfg.RsyncConfig.DestPath)
	options = append(options, "-azvh")
	options = append(options, src)
	options = append(options, dest)
	return options
}

func dumpStagingMysql(cfg Config) (string, error) {
	dumpPath := fmt.Sprintf(`mysql_staging_%s_%v.sql`, cfg.StagingConfig.Database, time.Now().Unix())
	options := dumpOptions(cfg.StagingConfig.Host, cfg.StagingConfig.Port, cfg.StagingConfig.User, cfg.StagingConfig.Password, cfg.StagingConfig.Database, cfg.StagingConfig.Tables)
	cmd := exec.Command(MysqlDump, options...)
	log.Println(cmd.Args)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	err = cmd.Start()
	if err != nil {
		return "", err
	}
	data, err := ioutil.ReadAll(stdout)
	if err != nil {
		return "", err
	}
	err = ioutil.WriteFile(dumpPath, data, 0644)
	if err != nil {
		return "", err
	}
	err = cmd.Wait()
	if err != nil {
		return "", err
	}
	return dumpPath, nil
}

func dumpProdMysql(cfg Config) (string, error) {
	dumpPath := fmt.Sprintf(`mysql_prod_%s_%v.sql`, cfg.ProdConfig.Database, time.Now().Unix())
	options := dumpOptions(cfg.ProdConfig.Host, cfg.ProdConfig.Port, cfg.ProdConfig.User, cfg.ProdConfig.Password, cfg.ProdConfig.Database, cfg.ProdConfig.Tables)

	cmd := exec.Command(MysqlDump, options...)
	log.Println(cmd.Args)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	err = cmd.Start()
	if err != nil {
		return "", err
	}
	data, err := ioutil.ReadAll(stdout)
	if err != nil {
		return "", err
	}
	err = ioutil.WriteFile(dumpPath, data, 0644)
	if err != nil {
		return "", err
	}
	err = cmd.Wait()
	if err != nil {
		return "", err
	}
	return dumpPath, nil
}

func dumpOptions(host string, port int, user, password, database string, tables []string) []string {
	var options []string
	options = append(options, fmt.Sprintf(`-h%v`, host))
	options = append(options, fmt.Sprintf(`-P%v`, port))
	options = append(options, fmt.Sprintf(`-u%v`, user))
	if password != "" {
		options = append(options, fmt.Sprintf(`-p%v`, password))
	}
	options = append(options, database)
	if len(tables) > 0 {
		options = append(options, tables...)
	}

	return options
}

func copyProdMysql(cfg Config, prodSource, stagingSource string) error {
	prodDS := fmt.Sprintf("%s:%s@tcp(%s:%d)/", cfg.ProdConfig.User, cfg.ProdConfig.Password, cfg.ProdConfig.Host, cfg.ProdConfig.Port)
	db, err := sql.Open("mysql", prodDS)
	if err != nil {
		return errors.Wrap(err, "connect prod mysql failed")
	}
	prodDumper := NewProdDumper(db)
	err = prodDumper.Ping()
	if err != nil {
		return errors.Wrap(err, "ping prod mysql failed")
	}
	err = prodDumper.DuplicateDatabase(cfg.ProdConfig.DuplicateDatabase)
	if err != nil {
		return errors.Wrap(err, "duplicate prod mysql failed")
	}

	return nil
}

func importMysql(cfg Config, prodSource, stagingSource string) error {
	prodBytes, err := ioutil.ReadFile(prodSource)
	if err != nil {
		return err
	}
	stagingBytes, err := ioutil.ReadFile(stagingSource)
	if err != nil {
		return err
	}
	options := importOptions(cfg.ProdConfig.Host, cfg.ProdConfig.Port, cfg.ProdConfig.User, cfg.ProdConfig.Password, cfg.ProdConfig.DuplicateDatabase)
	cmd := exec.Command(Mysql, options...)
	stdin, err := cmd.StdinPipe()

	go func() {
		defer stdin.Close()
		io.WriteString(stdin, string(prodBytes))
		io.WriteString(stdin, string(stagingBytes))
	}()

	data, err := cmd.CombinedOutput()
	if err != nil {
		log.Println(string(data))
		return err
	}
	return nil
}

func importOptions(host string, port int, user, password string, dupDB string) []string {
	var options []string
	options = append(options, fmt.Sprintf(`-h%v`, host))
	options = append(options, fmt.Sprintf(`-P%v`, port))
	options = append(options, fmt.Sprintf(`-u%v`, user))
	if password != "" {
		options = append(options, fmt.Sprintf(`-p%v`, password))
	}
	options = append(options, dupDB)
	return options
}

func modifyConfig(cfg Config) error {
	options := sshOptions(cfg.SSHConfig.Port, cfg.SSHConfig.User, cfg.SSHConfig.Host)
	cmd := exec.Command(SSH, options...)
	log.Println(cmd.Args)
	stdin, err := cmd.StdinPipe()
	go func() {
		defer stdin.Close()
		io.WriteString(stdin, fmt.Sprintf(`sed -i 's#\(database_name:\).*#\1 %s#' /mnt/gv0/config/parameters-srv1.yml && sed -i 's#\(database_name:\).*#\1 %s#' /mnt/gv0/config/parameters-srv2.yml && sed -i 's#\(database_name:\).*#\1 %s#' /mnt/gv0/config/parameters-srv3.yml`, cfg.ProdConfig.DuplicateDatabase, cfg.ProdConfig.DuplicateDatabase, cfg.ProdConfig.DuplicateDatabase))
	}()

	data, err := cmd.CombinedOutput()
	if err != nil {
		log.Println(string(data))
		return err
	}

	return nil
}

func cc(port int, user, host string) error {
	options := sshOptions(port, user, host)
	cmd := exec.Command(SSH, options...)
	log.Println(cmd.Args)
	stdin, err := cmd.StdinPipe()
	go func() {
		defer stdin.Close()
		io.WriteString(stdin, `/usr/local/bin/php -d memory_limit=-1 /symfony/app/console cache:clear --env=prod && chmod -R 777 /symfony/app/logs /symfony/app/cache /symfony/web/uploads`)
	}()

	data, err := cmd.CombinedOutput()
	if err != nil {
		log.Println(string(data))
		return err
	}

	return nil
}

func sshOptions(port int, user, host string) []string {
	var options []string
	options = append(options, fmt.Sprintf("-p %d", port))
	options = append(options, "-T")
	options = append(options, fmt.Sprintf("%s@%s", user, host))
	return options
}

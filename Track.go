package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
	"gopkg.in/yaml.v3"
)

var db *sql.DB
var logfile string

type Config struct {
	Path     string   `yaml:"path"`
	Commands []string `yaml:"commands"`
	Exclude  []string `yaml:"exclude_regex"`
	Include  []string `yaml:"include_regex"`
	Logfile  string   `yaml:"log"`
}

func main() {
	var err error
	connectDB()
	defer db.Close()

	fmt.Println("Monitor Tool 0.1")
	fmt.Println("reading config ... ")
	config := readConfig()

	fmt.Println("config loaded ... ")

	fmt.Println("monitoring ... ")

	checkErr(err)
	monitoring(config)

}

func loadEnvVar(envVar string) string {
	err := godotenv.Load(".env")
	checkErr(err)
	return os.Getenv(envVar)
}

func connectDB() {
	var err error
	host := loadEnvVar("DBhost")
	port := loadEnvVar("DBport")
	user := loadEnvVar("DBuser")
	password := loadEnvVar("DBpassword")
	dbname := loadEnvVar("DBname")

	psqlconn := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable", host, port, user, password, dbname)

	db, err = sql.Open("postgres", psqlconn)
	checkErr(err)

	err = db.Ping()
	checkErr(err)
}

/*
Executing commands on the shell and exits when a command fails
param commands []string
*/
func execCommands(config Config) {
	for i := range config.Commands {
		os.Chdir(config.Path)
		args := strings.Split(config.Commands[i], " ")
		cmd := exec.Command(args[0], args[1:]...)
		cmdOut, err := cmd.Output()
		writeLog(string(cmdOut), config)
		writeErrorLog(err, config)
		logErr(err)
		if err != nil {
			return
		}
	}
	fmt.Println("Finished")
}

/*
reads the content of a local file and returns it
@param filename string
@param path string
@return string
*/

func getLocalFileContent(filename string, config Config) string {
	// read a files content
	fileByte, err := os.ReadFile(config.Path + filename)
	writeErrorLog(err, config)
	checkErr(err)
	fileString := string(fileByte)

	return fileString
}

/*
compares the content of two strings and returns true if they are different false if not
@param fileContent1 string
@param fileContent2 string
@return bool
*/
func contentDiffers(fileContent1 string, fileContent2 string) bool {
	// compare the content of two files
	if fileContent1 != fileContent2 {
		return true
	} else {
		return false
	}
}

/*
Selects the latest known version of a file by its path and name from a database table and returns the content of the file

@param filename string
@param path string
@return string
*/
func getLatestVersion(filename string, config Config) string {
	// select from database the content of your file
	var content string = ""
	rows, err := db.Query("SELECT file_content "+
		"FROM changes "+
		"WHERE path = $1 "+
		"AND filename = $2 "+
		"ORDER BY id desc "+
		"LIMIT 1;", config.Path, filename)
	defer rows.Close()
	writeErrorLog(err, config)
	checkErr(err)

	for rows.Next() {
		err = rows.Scan(&content)
		writeErrorLog(err, config)
		checkErr(err)
	}
	return content
}

/*
Insert the latest known version of a file to the database
@param filename string
@param path string
@param content string
*/
func saveChangesToDb(filename string, config Config, content string) {
	// insert into database table the content of a file
	file, err := os.Stat(config.Path + filename)
	writeErrorLog(err, config)
	checkErr(err)

	insertStat := "INSERT INTO changes (path, filename, file_content, created_at) VALUES ($1, $2, $3, $4);"
	_, err = db.Exec(insertStat, config.Path, filename, content, file.ModTime())
	writeErrorLog(err, config)
	checkErr(err)
}

/*
infinite loop function to monitor changes
@param config []Config
*/
func monitoring(config []Config) {
	for {
		for i := range config {
			writeLog("Tracking: "+config[i].Path, config[i])
			files := readDir(config[i])

			checkChanges(files, config[i])
		}
		time.Sleep(10 * time.Second)
	}
}

/*
*
The heart of the program, if a file changed execute commands and save the changes on success
@param files []string
@param path string
@param commands []string
*/
func checkChanges(files []string, config Config) {
	for i := range files {
		fileContent := getLatestVersion(files[i], config)
		localFileContent := getLocalFileContent(files[i], config)
		if contentDiffers(fileContent, localFileContent) {
			execCommands(config)
			saveChangesToDb(files[i], config, localFileContent)
		}
	}
}

/*
reads config and returns a array of Config
@return []Config
*/
func readConfig() []Config {

	var config []Config
	file, err := os.ReadFile("Config.yaml")
	checkErr(err)

	err = yaml.Unmarshal(file, &config)
	checkErr(err)

	return config
}

/*
Reading the content of a directory and returns a list of files
@param path string
@return []string
*/
func readDir(config Config) []string {
	var files []string
	var match bool
	var addFile bool = true

	f, err := os.Open(config.Path)
	writeErrorLog(err, config)
	checkErr(err)

	file, err := f.Readdir(0)
	writeErrorLog(err, config)
	checkErr(err)

	for _, v := range file {
		if !v.IsDir() {
			for i := range config.Exclude {
				match, _ = regexp.MatchString(config.Exclude[i], v.Name())
				if match {
					addFile = false
				}
			}
			for i := range config.Include {
				match, _ = regexp.MatchString(config.Include[i], v.Name())
				if match {
					addFile = true
				}
			}
			if addFile {
				writeLog("File tracked: "+v.Name(), config)
				files = append(files, v.Name())
			}
		}
		addFile = true
	}
	return files
}

/*
Checks errors and logs them
@param err error
*/
func checkErr(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

func logErr(err error) {
	if err != nil {
		log.Print(err)
	}
}

func writeLog(note string, config Config) {
	var err error
	file, _ := os.OpenFile(config.Logfile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)

	defer file.Close()
	//file.WriteString(note)
	_, err = fmt.Fprintln(file, note)
	logErr(err)
}

func writeErrorLog(err error, config Config) {
	file, _ := os.OpenFile(config.Logfile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)

	defer file.Close()

	if err != nil {
		fmt.Fprintln(file, err)
	}
}

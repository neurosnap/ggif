package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/atotto/clipboard"
	"github.com/op/go-logging"
)

var log = logging.MustGetLogger("app")

// Example format string. Everything except the message has a custom color
// which is dependent on the log level. Many fields have a custom output
// formatting too, eg. the time returns the hour down to the milli second.
var format = logging.MustStringFormatter(
	`%{color}%{time:15:04:05.000} %{shortfunc} ▶ %{level:.4s} %{id:03x}%{color:reset} %{message}`,
)

// Config is a configuration object for cli
type Config struct {
	Width    int    `json:"width"`
	Frames   int    `json:"frames"`
	Quality  int    `json:"quality"`
	Src      string `json:"src"`
	Dist     string `json:"dist"`
	Bucket   string `json:"bucket"`
	LogLevel string `json:"log_level"`
}

func loadConfig(file string) Config {
	var config Config
	configFile, err := os.Open(file)
	defer configFile.Close()
	if err != nil {
		log.Fatal(err.Error())
	}
	jsonParser := json.NewDecoder(configFile)
	jsonParser.Decode(&config)
	return config
}

func printError(err error) {
	if err != nil {
		log.Error(err.Error())
	}
}

func printOutput(outs []byte) {
	if len(outs) > 0 {
		log.Debug(string(outs))
	}
}

func findNewestFile(dir string) string {
	files, _ := ioutil.ReadDir(dir)
	var newestFile string
	var newestTime int64 = 0
	for _, f := range files {
		fi, err := os.Stat(filepath.Join(dir, f.Name()))
		if err != nil {
			log.Error(err.Error())
			continue
		}
		currTime := fi.ModTime().Unix()
		if currTime > newestTime {
			newestTime = currTime
			newestFile = f.Name()
		}
	}
	return filepath.Join(dir, newestFile)
}

func runCmd(name string, arg ...string) {
	cmd := exec.Command(name, arg...)
	log.Debug(cmd.Args)
	output, err := cmd.CombinedOutput()
	printOutput(output)
	printError(err)
}

func createTmpDir() string {
	dir, err := ioutil.TempDir("/tmp", "pngs")
	if err != nil {
		log.Fatal(err)
	}

	return dir
}

func main() {
	config := loadConfig("./config.json")
	level, err := logging.LogLevel(config.LogLevel)
	if err != nil {
		log.Fatal(err.Error())
	}
	logging.SetLevel(level, "app")
	log.Debugf("%+v\n", config)

	srcDir := config.Src
	videoFile := ""
	if len(os.Args) >= 2 {
		videoFile = os.Args[1]
	} else {
		videoFile = findNewestFile(srcDir)
	}

	if videoFile == "" {
		log.Fatal("No file specified and no file found in config.Src, exiting")
	}

	tmpDir := createTmpDir()
	defer os.RemoveAll(tmpDir)

	tmpfn := filepath.Join(tmpDir, "frame%04d.png")
	runCmd("ffmpeg", "-i", videoFile, tmpfn)

	newName := time.Now().Unix()
	infn := filepath.Join(tmpDir, "*.png")
	outputFile := fmt.Sprintf("%d.gif", newName)
	outfn := filepath.Join(config.Dist, outputFile)

	cmdin := fmt.Sprintf(
		"gifski -W %d -r %d -Q %d -o %s %s",
		config.Width,
		config.Frames,
		config.Quality,
		outfn,
		infn,
	)
	runCmd("/bin/sh", "-c", cmdin)

	if config.Bucket != "" {
		runCmd("gsutil", "cp", outfn, fmt.Sprintf("gs://%s", config.Bucket))
		url := fmt.Sprintf(
			"https://storage.googleapis.com/%s/%s",
			config.Bucket,
			outputFile,
		)
		fmt.Println(url)
		clipboard.WriteAll(url)
	}
}

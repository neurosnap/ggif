package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	"github.com/fsnotify/fsnotify"
	"github.com/h2non/filetype"
	"github.com/op/go-logging"
	"github.com/urfave/cli/v2"
	"github.com/urfave/cli/v2/altsrc"
)

var log = logging.MustGetLogger("app")
var format = logging.MustStringFormatter(
	`%{color} %{shortfile} ▶ %{level:.4s} %{id:03x}%{color:reset} %{message}`,
)

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
		fname := filepath.Join(dir, f.Name())
		fi, err := os.Stat(fname)
		buf, _ := ioutil.ReadFile(fname)
		if !filetype.IsVideo(buf) {
			continue
		}
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

func initLogging(c *cli.Context) {
	level, err := logging.LogLevel(c.String("log"))
	if err != nil {
		log.Fatal(err.Error())
	}
	logging.SetLevel(level, "app")
}

func uploadGCP(bucket string, videoFile string, bucketFile string) {
	if bucket == "" {
		return
	}

	runCmd("gsutil", "cp", videoFile, fmt.Sprintf("gs://%s", bucket))
	url := fmt.Sprintf(
		"https://storage.googleapis.com/%s/%s",
		bucket,
		bucketFile,
	)
	fmt.Println(url)
	clipboard.WriteAll(url)
}

func uploadS3(bucket string, videoFile string, bucketFile string) {
	if bucket == "" {
		return
	}

	runCmd("aws", "s3", "cp", videoFile, fmt.Sprintf("s3://%s/%s", bucket, bucketFile), "--acl", "public-read")
	url := fmt.Sprintf(
		"https://%s.s3.amazonaws.com/%s",
		bucket,
		bucketFile,
	)
	fmt.Println(url)
	clipboard.WriteAll(url)
}

func findConfigFile() string {
	user, err := user.Current()
	if err != nil {
		log.Debug(err)
	}
	fname := filepath.Join(user.HomeDir, ".ggif.json")
	if _, err := os.Stat(fname); err == nil {
		return fname
	}
	return ""
}

func watch(c *cli.Context) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatal(err)
	}
	defer watcher.Close()

	log.Debugf("Watching %s", c.String("src"))

	done := make(chan bool)
	go func() {
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				log.Debug("event:", event)
				if event.Op&fsnotify.Create == fsnotify.Create {
					log.Debug("modified file:", event.Name)
					process(c, event.Name)
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				log.Debug("error:", err)
			}
		}
	}()

	err = watcher.Add(c.String("src"))
	if err != nil {
		log.Fatal(err)
	}
	<-done
}

func process(c *cli.Context, videoFile string) {
	if videoFile == "" {
		log.Fatal("No file specified and no file found in config.Src, exiting")
	}

	ext := filepath.Ext(videoFile)
	videoFileName := strings.TrimSuffix(filepath.Base(videoFile), ext)
	bucketFile := fmt.Sprintf("%s_%d%s", videoFileName, time.Now().Unix(), ext)

	uploadGCP(c.String("gcp-bucket"), videoFile, bucketFile)
	uploadS3(c.String("s3-bucket"), videoFile, bucketFile)
}

func main() {
	logging.SetFormatter(format)
	configFile := findConfigFile()

	curDir, err := filepath.Abs(filepath.Dir(os.Args[0]))
	if err != nil {
		log.Fatal(err)
	}

	flags := []cli.Flag{
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:  "log",
			Value: "ERROR",
			Usage: "log level for output",
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:  "src",
			Value: curDir,
			Usage: "source folder for movie file",
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:  "gcp-bucket",
			Value: "",
			Usage: "google cloud storage bucket name",
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:  "s3-bucket",
			Value: "",
			Usage: "aws s3 bucket name",
		}),
		&cli.StringFlag{
			Name:  "load",
			Value: configFile,
			Usage: "location and file name of configuration file",
		},
		&cli.BoolFlag{
			Name:  "watch",
			Value: false,
			Usage: "watch src directory for new files",
		},
	}

	app := &cli.App{
		Name:   "ggif",
		Usage:  "convert movies to gifs and upload them",
		Flags:  flags,
		Before: altsrc.InitInputSourceWithContext(flags, altsrc.NewJSONSourceFromFlagFunc("load")),
		Action: func(c *cli.Context) error {
			initLogging(c)
			if c.Bool("watch") {
				watch(c)
			} else {
				videoFile := ""
				if c.Args().Len() >= 1 {
					videoFile = c.Args().Get(0)
				} else {
					videoFile = findNewestFile(c.String("src"))
				}
				process(c, videoFile)
			}

			return nil
		},
	}

	err = app.Run(os.Args)
	if err != nil {
		log.Fatal(err.Error())
	}
}

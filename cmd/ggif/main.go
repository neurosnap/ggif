package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"time"

	"github.com/atotto/clipboard"
	"github.com/fsnotify/fsnotify"
	"github.com/h2non/filetype"
	"github.com/op/go-logging"
	"github.com/urfave/cli/v2"
	"github.com/urfave/cli/v2/altsrc"
)

var log = logging.MustGetLogger("app")

// Example format string. Everything except the message has a custom color
// which is dependent on the log level. Many fields have a custom output
// formatting too, eg. the time returns the hour down to the milli second.
var format = logging.MustStringFormatter(
	`%{color}%{time:15:04:05.000} %{shortfunc} ▶ %{level:.4s} %{id:03x}%{color:reset} %{message}`,
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

func createTmpDir() string {
	dir, err := ioutil.TempDir("/tmp", "pngs")
	if err != nil {
		log.Fatal(err)
	}

	return dir
}

func initLogging(c *cli.Context) {
	level, err := logging.LogLevel(c.String("log"))
	if err != nil {
		log.Fatal(err.Error())
	}
	logging.SetLevel(level, "app")
}

func createGif(c *cli.Context, tmpDir string, outfn string) {
	infn := filepath.Join(tmpDir, "*.png")

	cmdin := fmt.Sprintf(
		"gifski -W %d -r %d -Q %d -o %s %s",
		c.Int("width"),
		c.Int("frames"),
		c.Int("quality"),
		outfn,
		infn,
	)
	runCmd("/bin/sh", "-c", cmdin)
}

func uploadGCP(bucket string, outfn string, outputFile string) {
	if bucket == "" {
		return
	}

	runCmd("gsutil", "cp", outfn, fmt.Sprintf("gs://%s", bucket))
	url := fmt.Sprintf(
		"https://storage.googleapis.com/%s/%s",
		bucket,
		outputFile,
	)
	fmt.Println(url)
	clipboard.WriteAll(url)
}

func findConfigFile() string {
	user, err := user.Current()
	if err != nil {
		log.Debug(err)
	}
	return filepath.Join(user.HomeDir, ".ggif.json")
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

	tmpDir := createTmpDir()
	defer os.RemoveAll(tmpDir)

	tmpfn := filepath.Join(tmpDir, "frame%04d.png")
	runCmd("ffmpeg", "-i", videoFile, tmpfn)

	newName := time.Now().Unix()
	outputFile := fmt.Sprintf("%d.gif", newName)
	distDir := c.String("dist")
	if distDir == "" {
		distDir = c.String("src")
	}
	outfn := filepath.Join(distDir, outputFile)

	createGif(c, tmpDir, outfn)
	uploadGCP(c.String("bucket"), outfn, outputFile)
}

func main() {
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
		altsrc.NewIntFlag(&cli.IntFlag{
			Name:  "quality",
			Value: 100,
			Usage: "quality of gif (1-100)",
		}),
		altsrc.NewIntFlag(&cli.IntFlag{
			Name:  "frames",
			Value: 20,
			Usage: "framerate for gif",
		}),
		altsrc.NewIntFlag(&cli.IntFlag{
			Name:  "width",
			Value: 960,
			Usage: "width resolution for gif",
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:  "src",
			Value: curDir,
			Usage: "source folder for movie file",
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:  "dist",
			Value: "",
			Usage: "destination folder folder for gif file",
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:  "bucket",
			Value: "",
			Usage: "google cloud storage bucket name",
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
		log.Fatal(err)
	}
}

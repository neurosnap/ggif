# ggif

Convert movies to gifs and upload to GCP

Place config file in home directory `.ggif.json`

## Requirements

- ffmpeg
- gifski
- gsutil

## Getting started

```bash
go get github.com/neurosnap/ggif/cmd/ggif
go install github.com/neurosnap/ggif/cmd/ggif
```

## Usage

```bash
# grab the newest file inside src folder
ggif
```

```bash
ggif <file>.mov
```

```bash
ggif help
```

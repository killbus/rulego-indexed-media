// media-tool runs the pinned service's FFmpeg or FFprobe through the same
// protocol client as the candidate plugin. CI builds it; no local binaries or
// an assumed ffprobe executable in the upstream CLI image are needed.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	ffmpegclient "github.com/killbus/rulego-ffmpeg-over-ip/client"
)

func main() {
	program := flag.String("program", "ffprobe", "remote program")
	flag.Parse()
	if *program == "staging" {
		if err := inspectStaging(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	config := ffmpegclient.Config{Address: "ffoip:5050", AuthSecret: "fixture-secret", DialTimeout: 5 * time.Second}
	invocation := ffmpegclient.Invocation{Program: *program, Args: flag.Args(), MaxOutputBytes: 32 << 20}
	var writeErr error
	_, err := ffmpegclient.Run(ctx, config, invocation, func(channel string, data []byte) {
		output := os.Stdout
		if channel == "stderr" {
			output = os.Stderr
		}
		if writeErr == nil {
			_, writeErr = output.Write(data)
			if writeErr != nil {
				cancel()
			}
		}
	})
	if writeErr != nil {
		err = writeErr
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// Origin directories are private to the runtime UID. Inspect them from the
// same pinned image/user instead of changing permissions for the host runner.
func inspectStaging() error {
	if flag.NArg() != 1 {
		return fmt.Errorf("staging inspection requires one directory")
	}
	entries, err := os.ReadDir(flag.Arg(0))
	if err != nil {
		return err
	}
	files := make([]map[string]any, 0, len(entries))
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			return err
		}
		files = append(files, map[string]any{"name": entry.Name(), "bytes": info.Size(), "regular": info.Mode().IsRegular()})
	}
	return json.NewEncoder(os.Stdout).Encode(files)
}

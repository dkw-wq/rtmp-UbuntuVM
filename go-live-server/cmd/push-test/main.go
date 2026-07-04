package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

type config struct {
	pushURL      string
	rtmpBase     string
	streamKey    string
	token        string
	size         string
	fps          int
	videoBitrate string
	audioBitrate string
	audioFreq    int
	gop          int
	duration     time.Duration
	fileDuration time.Duration
	ffmpeg       string
	makeMP4      string
	file         string
	noAudio      bool
	noTimer      bool
	preset       string
	extraLabel   string
	printCommand bool
}

func main() {
	cfg := parseFlags()

	if err := run(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "push-test: %v\n", err)
		os.Exit(1)
	}
}

func parseFlags() config {
	cfg := config{}
	flag.StringVar(&cfg.pushURL, "push-url", "", "full RTMP push URL, for example rtmp://host:1935/live/<stream_key>?token=<push_token>")
	flag.StringVar(&cfg.rtmpBase, "rtmp-base", "rtmp://127.0.0.1:1935/live", "RTMP base URL used with -stream-key and -token")
	flag.StringVar(&cfg.streamKey, "stream-key", "", "server-generated stream_key")
	flag.StringVar(&cfg.token, "token", "", "server-generated publish token")
	flag.StringVar(&cfg.size, "size", "1280x720", "test video size")
	flag.IntVar(&cfg.fps, "fps", 30, "test video frame rate")
	flag.StringVar(&cfg.videoBitrate, "video-bitrate", "2000k", "video bitrate")
	flag.StringVar(&cfg.audioBitrate, "audio-bitrate", "128k", "audio bitrate")
	flag.IntVar(&cfg.audioFreq, "audio-freq", 1000, "synthetic sine audio frequency")
	flag.IntVar(&cfg.gop, "gop", 60, "GOP/keyframe interval in frames; use 2 seconds, e.g. 60 for 30 fps")
	flag.DurationVar(&cfg.duration, "duration", 0, "push duration, for example 60s; 0 means run until interrupted")
	flag.DurationVar(&cfg.fileDuration, "file-duration", 30*time.Second, "duration for -make-mp4 generated file")
	flag.StringVar(&cfg.ffmpeg, "ffmpeg", "ffmpeg", "ffmpeg binary path")
	flag.StringVar(&cfg.makeMP4, "make-mp4", "", "generate a local MP4 test file before pushing; if no push URL is provided, only generate the file")
	flag.StringVar(&cfg.file, "file", "", "existing media file to loop and push instead of live synthetic source")
	flag.BoolVar(&cfg.noAudio, "no-audio", false, "disable synthetic audio")
	flag.BoolVar(&cfg.noTimer, "no-timer", false, "disable drawtext timer overlay")
	flag.StringVar(&cfg.preset, "preset", "veryfast", "libx264 preset")
	flag.StringVar(&cfg.extraLabel, "label", "SRS PUSH TEST", "timer overlay label")
	flag.BoolVar(&cfg.printCommand, "print-command", false, "print ffmpeg command before running")
	flag.Parse()
	return cfg
}

func run(cfg config) error {
	if cfg.fps <= 0 {
		return errors.New("-fps must be greater than 0")
	}
	if cfg.gop <= 0 {
		cfg.gop = cfg.fps * 2
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if cfg.makeMP4 != "" {
		if err := generateMP4(ctx, cfg); err != nil {
			return err
		}
		cfg.file = cfg.makeMP4
	}

	pushURL, ok := resolvePushURL(cfg)
	if !ok {
		if cfg.makeMP4 != "" {
			fmt.Printf("generated test file: %s\n", cfg.makeMP4)
			return nil
		}
		return errors.New("provide -push-url, or provide both -stream-key and -token")
	}

	args := buildPushArgs(cfg, pushURL)
	fmt.Printf("pushing to %s\n", redactURL(pushURL))
	fmt.Println("press Ctrl+C to stop")
	return runFFmpeg(ctx, cfg.ffmpeg, args, cfg.printCommand)
}

func resolvePushURL(cfg config) (string, bool) {
	if cfg.pushURL != "" {
		return cfg.pushURL, true
	}
	if cfg.streamKey == "" || cfg.token == "" {
		return "", false
	}
	base := strings.TrimRight(cfg.rtmpBase, "/")
	return fmt.Sprintf("%s/%s?token=%s", base, cfg.streamKey, cfg.token), true
}

func generateMP4(ctx context.Context, cfg config) error {
	if cfg.fileDuration <= 0 {
		return errors.New("-file-duration must be greater than 0")
	}

	args := []string{"-hide_banner", "-y"}
	args = append(args, syntheticInputs(cfg, false)...)
	args = append(args, "-t", formatDuration(cfg.fileDuration))
	args = append(args, syntheticEncodingArgs(cfg, false)...)
	args = append(args, "-movflags", "+faststart", cfg.makeMP4)

	fmt.Printf("generating %s (%s)\n", cfg.makeMP4, cfg.fileDuration)
	return runFFmpeg(ctx, cfg.ffmpeg, args, cfg.printCommand)
}

func buildPushArgs(cfg config, pushURL string) []string {
	args := []string{"-hide_banner"}

	if cfg.file != "" {
		args = append(args, "-stream_loop", "-1", "-re", "-i", cfg.file)
		if cfg.duration > 0 {
			args = append(args, "-t", formatDuration(cfg.duration))
		}
		args = append(args, fileEncodingArgs(cfg)...)
	} else {
		args = append(args, syntheticInputs(cfg, true)...)
		if cfg.duration > 0 {
			args = append(args, "-t", formatDuration(cfg.duration))
		}
		args = append(args, syntheticEncodingArgs(cfg, true)...)
	}

	args = append(args, "-f", "flv", pushURL)
	return args
}

func syntheticInputs(cfg config, realtime bool) []string {
	inputs := []string{}
	if realtime {
		inputs = append(inputs, "-re")
	}
	inputs = append(inputs,
		"-f", "lavfi",
		"-i", fmt.Sprintf("testsrc2=size=%s:rate=%d", cfg.size, cfg.fps),
	)
	if !cfg.noAudio {
		inputs = append(inputs,
			"-f", "lavfi",
			"-i", fmt.Sprintf("sine=frequency=%d:sample_rate=44100", cfg.audioFreq),
		)
	}
	return inputs
}

func syntheticEncodingArgs(cfg config, live bool) []string {
	filter := videoFilter(cfg)
	args := []string{}
	if filter != "" {
		args = append(args, "-filter_complex", filter, "-map", "[v]")
	} else {
		args = append(args, "-map", "0:v")
	}
	if !cfg.noAudio {
		args = append(args, "-map", "1:a")
	}

	args = append(args,
		"-c:v", "libx264",
		"-preset", cfg.preset,
		"-tune", "zerolatency",
		"-pix_fmt", "yuv420p",
		"-b:v", cfg.videoBitrate,
		"-maxrate", cfg.videoBitrate,
		"-bufsize", bitrateBufSize(cfg.videoBitrate),
		"-g", fmt.Sprint(cfg.gop),
		"-keyint_min", fmt.Sprint(cfg.gop),
	)
	if !cfg.noAudio {
		args = append(args, "-c:a", "aac", "-b:a", cfg.audioBitrate, "-ar", "44100")
	} else {
		args = append(args, "-an")
	}
	if live {
		args = append(args, "-flvflags", "no_duration_filesize")
	} else {
		args = append(args, "-shortest")
	}
	return args
}

func fileEncodingArgs(cfg config) []string {
	args := []string{
		"-c:v", "libx264",
		"-preset", cfg.preset,
		"-tune", "zerolatency",
		"-pix_fmt", "yuv420p",
		"-b:v", cfg.videoBitrate,
		"-maxrate", cfg.videoBitrate,
		"-bufsize", bitrateBufSize(cfg.videoBitrate),
		"-g", fmt.Sprint(cfg.gop),
		"-keyint_min", fmt.Sprint(cfg.gop),
	}
	if cfg.noAudio {
		return append(args, "-an")
	}
	return append(args, "-c:a", "aac", "-b:a", cfg.audioBitrate, "-ar", "44100")
}

func videoFilter(cfg config) string {
	base := "format=yuv420p"
	if cfg.noTimer {
		return "[0:v]" + base + "[v]"
	}

	text := escapeDrawtext(cfg.extraLabel) + " %{pts\\:hms}"
	draw := fmt.Sprintf(
		"drawtext=text='%s':x=40:y=40:fontcolor=white:fontsize=44:box=1:boxcolor=black@0.60:boxborderw=12",
		text,
	)
	return "[0:v]" + base + "," + draw + "[v]"
}

func escapeDrawtext(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "'", "\\'")
	s = strings.ReplaceAll(s, ":", "\\:")
	return s
}

func bitrateBufSize(v string) string {
	if strings.HasSuffix(v, "k") || strings.HasSuffix(v, "K") {
		n := strings.TrimSuffix(strings.TrimSuffix(v, "k"), "K")
		var kbps int
		if _, err := fmt.Sscanf(n, "%d", &kbps); err == nil && kbps > 0 {
			return fmt.Sprintf("%dk", kbps*2)
		}
	}
	return v
}

func runFFmpeg(ctx context.Context, ffmpeg string, args []string, printCommand bool) error {
	if _, err := exec.LookPath(ffmpeg); err != nil {
		return fmt.Errorf("ffmpeg not found: %w", err)
	}
	if printCommand {
		fmt.Printf("%s %s\n", ffmpeg, shellJoin(args))
	}

	cmd := exec.CommandContext(ctx, ffmpeg, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return fmt.Errorf("ffmpeg failed: %w", err)
	}
	return nil
}

func formatDuration(d time.Duration) string {
	return fmt.Sprintf("%.3f", d.Seconds())
}

func redactURL(raw string) string {
	idx := strings.Index(raw, "token=")
	if idx < 0 {
		return raw
	}
	end := strings.IndexAny(raw[idx:], "&#")
	if end < 0 {
		return raw[:idx+6] + "***"
	}
	return raw[:idx+6] + "***" + raw[idx+end:]
}

func shellJoin(args []string) string {
	quoted := make([]string, 0, len(args))
	for _, a := range args {
		if a == "" || strings.ContainsAny(a, " \t\n'\"$&;()[]{}<>|*?!") {
			quoted = append(quoted, "'"+strings.ReplaceAll(a, "'", "'\\''")+"'")
		} else {
			quoted = append(quoted, a)
		}
	}
	return strings.Join(quoted, " ")
}

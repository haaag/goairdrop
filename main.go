package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
)

const appName = "goairdrop"

const filePerm = 0o644 // Permissions for new files.

var (
	logger   *slog.Logger
	logFname string
)

var (
	addrFlag    string
	versionFlag bool
	loggerFlag  string
)

var appVersion = "0.1.1"

// Message represents the structure of the incoming messages.
type Message struct {
	Type    string `json:"type"`
	Content string `json:"content"`
	Action  string `json:"action"`
}

// Response represents the structure of the outgoing messages.
type Response struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// notify sends a notification using the notify-send command.
func notify(title, message string) error {
	args := []string{
		"notify-send",
		"--app-name=" + appName,
		"--icon=gnome-user-share",
		title,
		message,
	}

	return executeCmd(args...)
}

// openURL opens a URL in the default browser.
func openURL(s string) error {
	args := osArgs()
	if err := executeCmd(append(args, s)...); err != nil {
		return fmt.Errorf("%w: opening in browser", err)
	}

	return notify(appName, "Opening URL: "+s)
}

// executeCmd runs a command with the given arguments and returns an error if
// the command fails.
func executeCmd(arg ...string) error {
	cmd := exec.CommandContext(context.Background(), arg[0], arg[1:]...)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("running command: %w", err)
	}

	return nil
}

// getEnv retrieves an environment variable.
//
// If the environment variable is not set, returns the default value.
func getEnv(s, def string) string {
	if v, ok := os.LookupEnv(s); ok {
		return v
	}

	return def
}

// expandHomeDir expands the home directory in the given string.
func expandHomeDir(s string) string {
	if strings.HasPrefix(s, "~/") {
		dirname, _ := os.UserHomeDir()
		s = filepath.Join(dirname, s[2:])
	}

	return s
}

// osArgs returns the correct arguments for the OS.
func osArgs() []string {
	// FIX: only support linux
	var args []string
	switch runtime.GOOS {
	case "darwin":
		args = append(args, "open")
	case "windows":
		args = append(args, "cmd", "/C", "start")
	default:
		args = append(args, "xdg-open")
	}

	return args
}

// handleOpenAction opens the URL in the default browser.
func handleOpenAction(msg Message) Response {
	resp := Response{
		Success: true,
		Message: "Opened text: " + msg.Content,
	}

	err := openURL(msg.Content)
	if err != nil {
		resp.Success = false
		resp.Message = "Error opening text: " + msg.Content
	}

	return resp
}

// getClientIP returns the IP address of the client.
func getClientIP(r *http.Request) string {
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		return strings.SplitN(forwarded, ",", 2)[0]
	}

	return r.RemoteAddr
}

// webhookHandler handles the incoming webhook requests.
func webhookHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	clientIP := getClientIP(r)

	logger.Info("Received request",
		slog.String("ip", clientIP),
		slog.String("user_agent", r.UserAgent()),
		slog.String("path", r.URL.Path),
	)

	var msg Message
	if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
		http.Error(w, "Error decoding JSON", http.StatusBadRequest)
		logger.Error("Error decoding JSON", slog.String("error", err.Error()))
		return
	}

	logger.Info("Received action", slog.String("action", msg.Action), slog.String("ip", clientIP))

	var resp Response
	switch msg.Action {
	case "open":
		resp = handleOpenAction(msg)
	default:
		resp = Response{Success: false, Message: "Unknown action: %s" + msg.Action}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		logger.Error("Error encoding response", slog.String("error", err.Error()))
	}

	logger.Info("Sent response",
		slog.Bool("success", resp.Success),
		slog.String("message", resp.Message),
	)
}

// version returns the application version.
func version() string {
	return fmt.Sprintf("%s v%s %s/%s\n", appName, appVersion, runtime.GOOS, runtime.GOARCH)
}

// usage prints the usage message.
func usage() {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("Usage:  %s v%s [options]\n\n", appName, appVersion))
	sb.WriteString("\tSimple webhook server\n\n")
	sb.WriteString("Options:\n")
	sb.WriteString("  -a, -addr string\n\tHTTP service address (default \":5001\")\n")
	sb.WriteString("  -V, -version\n\tPrint version and exit\n")
	sb.WriteString("  -l, -log string\n\tLog filepath\n")
	sb.WriteString("  -h, -help\n\tPrint this help message\n")
	sb.WriteString("\nFiles:\n")
	sb.WriteString(fmt.Sprintf("\t%s\n", logFname))

	fmt.Fprint(os.Stderr, sb.String())
}

func setupInterruptHandler(
	ctx context.Context,
	shutdownFunc func(context.Context) error,
) context.Context {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)

	ctx, cancel := context.WithCancel(ctx)

	go func() {
		<-sigChan
		logger.Debug("Received signal, initiating graceful shutdown")

		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer shutdownCancel()

		if err := shutdownFunc(shutdownCtx); err != nil {
			logger.Error("Graceful shutdown failed", slog.String("error", err.Error()))
		}

		cancel()
	}()

	return ctx
}

func main() {
	f, err := os.OpenFile(logFname, os.O_APPEND|os.O_CREATE|os.O_WRONLY, filePerm)
	if err != nil {
		log.Fatalf("Failed to open log file: %v", err)
	}

	multiWriter := io.MultiWriter(f, os.Stdout)
	logger = slog.New(slog.NewJSONHandler(multiWriter, nil))

	server := &http.Server{
		Addr:         addrFlag,
		Handler:      nil,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}
	http.HandleFunc("/wh", webhookHandler)

	ctx := setupInterruptHandler(context.Background(), server.Shutdown)

	serverErr := make(chan error)
	go func() {
		logger.Info("Starting server", slog.String("addr", addrFlag))
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	select {
	case err := <-serverErr:
		logger.Error("Server error", slog.String("error", err.Error()))
	case <-ctx.Done():
		logger.Info("Server stopped gracefully")
	}

	if err := f.Close(); err != nil {
		logger.Error("Failed closing log file", slog.String("error", err.Error()))
	}
}

func init() {
	flag.StringVar(&addrFlag, "addr", ":5001", "HTTP service address")
	flag.BoolVar(&versionFlag, "version", false, "Print version and exit")
	flag.BoolVar(&versionFlag, "V", false, "Print version and exit")
	flag.StringVar(&loggerFlag, "log", "", "Log filepath")
	flag.StringVar(&loggerFlag, "l", "", "Log filepath")
	flag.Usage = usage

	localState := getEnv("XDG_STATE_HOME", expandHomeDir("~/.local/state"))
	logFname = filepath.Join(localState, appName+".json")

	flag.Parse()
	if versionFlag {
		fmt.Print(version())
		os.Exit(0)
	}

	if loggerFlag != "" {
		logFname = loggerFlag
	}
}

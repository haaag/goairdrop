package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os/exec"
	"runtime"
)

const appName = "goairdrop"

var version = "0.1.0"

// Message represents the structure of the incoming messages
type Message struct {
	Text   string `json:"text"`
	Action string `json:"action"`
}

// Response represents the structure of the outgoing messages
type Response struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// openURL opens a URL in the default browser.
func openURL(s string) error {
	args := osArgs()
	if err := executeCmd(append(args, s)...); err != nil {
		return fmt.Errorf("%w: opening in browser", err)
	}
	return nil
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

// osArgs returns the correct arguments for the OS.
func osArgs() []string {
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
		Message: fmt.Sprintf("Opened text: %s", msg.Text),
	}

	err := openURL(msg.Text)
	if err != nil {
		resp.Success = false
		resp.Message = fmt.Sprintf("Error opening text: %s", msg.Text)
	}

	return resp
}

// webhookHandler handles the incoming webhook requests.
func webhookHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	var msg Message
	if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
		http.Error(w, "Error decoding JSON", http.StatusBadRequest)
		return
	}

	var resp Response
	switch msg.Action {
	case "open":
		resp = handleOpenAction(msg)
	default:
		resp = Response{
			Success: false,
			Message: fmt.Sprintf("Unknown action: %s", msg.Action),
		}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Fatal(err)
	}
}

func main() {
	addr := flag.String("addr", ":5001", "HTTP service address")
	flag.Usage = func() {
		fmt.Println("Usage:")
		fmt.Printf("  %s v%s [options]\n", appName, version)
		fmt.Println("\nOptions:")
		flag.PrintDefaults()
	}

	flag.Parse()

	http.HandleFunc("/wh", webhookHandler)

	log.Printf("Server starting on %s", *addr)
	log.Fatal(http.ListenAndServe(*addr, nil))
}

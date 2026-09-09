package main

import (
	"context"
	"embed"
	"encoding/json"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"sync/atomic"
	"syscall"
	"time"
)

//go:embed web/*
var webFS embed.FS

const listenAddr = "127.0.0.1:27183"

var quitting atomic.Bool

func main() {
	ln, err := net.Listen("tcp", listenAddr)
	if err != nil {
		openBrowser("http://" + listenAddr + "/")
		return
	}

	sub, err := fs.Sub(webFS, "web")
	if err != nil {
		log.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/snapshot", handleSnapshot)
	mux.HandleFunc("/api/spin", handleSpin)
	mux.HandleFunc("/api/quit", handleQuit)
	mux.Handle("/", http.FileServer(http.FS(sub)))

	srv := &http.Server{Handler: mux}

	go func() {
		if os.Getenv("VITALS_NO_BROWSER") == "1" {
			return
		}
		time.Sleep(150 * time.Millisecond)
		openBrowser("http://" + listenAddr + "/")
	}()

	go func() {
		ch := make(chan os.Signal, 1)
		signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
		<-ch
		quitting.Store(true)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()

	if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func handleSnapshot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	snap := collect()
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(snap)
}

func handleSpin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	startSpin(8 * time.Second)
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"ok":true,"ms":8000}`))
}

func handleQuit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"ok":true}`))
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	go func() {
		time.Sleep(200 * time.Millisecond)
		os.Exit(0)
	}()
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}

var spinning atomic.Bool

func startSpin(d time.Duration) {
	if spinning.Swap(true) {
		return
	}
	n := runtime.NumCPU()
	end := time.Now().Add(d)
	for i := 0; i < n; i++ {
		go func() {
			x := 0.0
			for spinning.Load() && time.Now().Before(end) {
				x = x*1.0000001 + 1.0000001
				if x > 1e12 {
					x = 0
				}
			}
			_ = x
		}()
	}
	time.AfterFunc(d, func() { spinning.Store(false) })
}

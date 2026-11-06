package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"
	"time"

	"github.com/mark3labs/mcp-go/server"

	"github.com/MarcoValdiviesoB/mcp-datos-gob-cl/tools"
)

// version se inyecta al compilar: go build -ldflags "-X main.version=v0.2.0".
// Con `go install ...@vX` se toma de la info de build del módulo.
var version = "dev"

func init() {
	if version != "dev" {
		return
	}
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		version = bi.Main.Version
	}
}

func userAgent() string {
	return "mcp-datos-gob-cl/" + version + " (+https://github.com/MarcoValdiviesoB/mcp-datos-gob-cl)"
}

func main() {
	httpAddr := flag.String("http", "", "escuchar MCP por HTTP (Streamable HTTP) en esta dirección, ej: :8080. Sin esto usa stdio")
	showVersion := flag.Bool("version", false, "mostrar la versión y salir")
	flag.Parse()

	if *showVersion {
		fmt.Println("mcp-datos-gob-cl", version)
		return
	}

	s := tools.NewServer(tools.DefaultDeps(version, userAgent()))

	var err error
	if *httpAddr != "" {
		err = serveHTTP(s, *httpAddr)
	} else {
		err = server.NewStdioServer(s).Listen(context.Background(), os.Stdin, os.Stdout)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

// httpHandler expone MCP en /mcp y un /health para chequeos de vida.
func httpHandler(mcpHandler http.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/mcp", mcpHandler)
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"status":"ok","version":"`+version+`"}`+"\n")
	})
	return mux
}

// serveHTTP sirve MCP por Streamable HTTP hasta recibir SIGINT/SIGTERM.
func serveHTTP(s *server.MCPServer, addr string) error {
	mcpSrv := server.NewStreamableHTTPServer(s)
	srv := &http.Server{Addr: addr, Handler: httpHandler(mcpSrv), ReadHeaderTimeout: 10 * time.Second}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	errc := make(chan error, 1)
	go func() {
		fmt.Fprintf(os.Stderr, "mcp-datos-gob-cl %s escuchando en %s (MCP en /mcp, salud en /health)\n", version, addr)
		errc <- srv.ListenAndServe()
	}()
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = mcpSrv.Shutdown(shutdownCtx)
	if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"runtime/debug"
	"strconv"
	"time"
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

// maxBody limita lo que se lee de una respuesta (10 MB).
const maxBody = 10 << 20

// httpClient sin timeout global: cada intento lleva el suyo vía contexto.
var httpClient = &http.Client{}

// fuente describe cómo llamar a una API externa: nombre para los mensajes, timeout por intento y reintentos.
type fuente struct {
	nombre   string
	timeout  time.Duration
	intentos int
	espera   time.Duration // backoff inicial; se duplica en cada reintento
}

var fuenteCKAN = fuente{"datos.gob.cl", 15 * time.Second, 3, 500 * time.Millisecond}

// httpError es una respuesta HTTP no exitosa. No guarda la URL (puede llevar ?ticket=).
type httpError struct {
	fuente string
	status int
	body   []byte
}

func (e *httpError) Error() string {
	return fmt.Sprintf("%s respondió HTTP %d", e.fuente, e.status)
}

// netError es un error de red sin la URL.
type netError struct {
	fuente  string
	timeout bool
	causa   string
}

func (e *netError) Error() string {
	if e.timeout {
		return fmt.Sprintf("%s no respondió a tiempo; puede estar caída o lenta, reintentá en unos minutos", e.fuente)
	}
	return fmt.Sprintf("no se pudo conectar con %s (%s)", e.fuente, e.causa)
}

func retryable(status int) bool {
	return status == http.StatusTooManyRequests || status == http.StatusBadGateway ||
		status == http.StatusServiceUnavailable || status == http.StatusGatewayTimeout
}

// doGet hace un GET con User-Agent, timeout por intento, reintentos con backoff ante 429/502/503/504/timeout,
// y lectura acotada a maxBody. Respeta la cancelación de ctx.
func doGet(ctx context.Context, f fuente, rawURL string) ([]byte, error) {
	var lastErr error
	espera := f.espera
	for i := 0; i < max(f.intentos, 1); i++ {
		if i > 0 {
			if err := sleepCtx(ctx, espera); err != nil {
				return nil, err
			}
			espera *= 2
		}
		body, retry, err := getOnce(ctx, f, rawURL)
		if err == nil {
			return body, nil
		}
		lastErr = err
		if !retry || ctx.Err() != nil {
			break
		}
		var he *httpError
		if errors.As(err, &he) && he.status == http.StatusTooManyRequests {
			espera = max(espera, 2*time.Second)
		}
	}
	if ctx.Err() != nil {
		return nil, fmt.Errorf("consulta a %s cancelada", f.nombre)
	}
	return nil, lastErr
}

func getOnce(ctx context.Context, f fuente, rawURL string) (body []byte, retry bool, err error) {
	ctx, cancel := context.WithTimeout(ctx, f.timeout)
	defer cancel()
	r, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, false, fmt.Errorf("URL inválida para %s", f.nombre)
	}
	r.Header.Set("User-Agent", userAgent())
	r.Header.Set("Accept", "application/json")
	resp, err := httpClient.Do(r)
	if err != nil {
		return nil, true, toNetError(f, err)
	}
	defer resp.Body.Close()
	body, err = io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return nil, true, toNetError(f, err)
	}
	if len(body) > maxBody {
		return nil, false, fmt.Errorf("la respuesta de %s supera %d MB; acotá la consulta", f.nombre, maxBody>>20)
	}
	if resp.StatusCode != http.StatusOK {
		if len(body) > 4096 {
			body = body[:4096]
		}
		if s := resp.Header.Get("Retry-After"); s != "" {
			if n, e := strconv.Atoi(s); e == nil && n > 0 && n <= 10 {
				_ = sleepCtx(ctx, time.Duration(n)*time.Second)
			}
		}
		return nil, retryable(resp.StatusCode), &httpError{f.nombre, resp.StatusCode, body}
	}
	return body, false, nil
}

// toNetError traduce un error de transporte a uno sin URL (url.Error incluye la URL completa con el ticket).
func toNetError(f fuente, err error) error {
	var ne net.Error
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &ne) && ne.Timeout()) {
		return &netError{fuente: f.nombre, timeout: true}
	}
	var dnsErr *net.DNSError
	var opErr *net.OpError
	switch {
	case errors.As(err, &dnsErr):
		return &netError{fuente: f.nombre, causa: "no se resolvió el dominio"}
	case errors.As(err, &opErr):
		return &netError{fuente: f.nombre, causa: "conexión rechazada o cortada"}
	}
	return &netError{fuente: f.nombre, causa: "error de red"}
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

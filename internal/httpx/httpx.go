// Package httpx es el cliente HTTP compartido por todas las fuentes: User-Agent, timeout por intento,
// reintentos con backoff y errores que nunca incluyen la URL (puede llevar ?ticket=).
package httpx

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"time"
)

// MaxBody limita lo que se lee de una respuesta (10 MB).
const MaxBody = 10 << 20

// Fuente describe cómo llamar a una API externa: nombre para los mensajes, timeout por intento y reintentos.
type Fuente struct {
	Nombre   string
	Timeout  time.Duration
	Intentos int
	Espera   time.Duration // backoff inicial; se duplica en cada reintento
}

// Client hace los GET. El *http.Client va sin timeout global: cada intento lleva el suyo vía contexto.
type Client struct {
	HTTP      *http.Client
	UserAgent string
}

// New crea un cliente con el User-Agent dado.
func New(userAgent string) *Client {
	return &Client{HTTP: &http.Client{}, UserAgent: userAgent}
}

// HTTPError es una respuesta HTTP no exitosa. No guarda la URL (puede llevar ?ticket=).
type HTTPError struct {
	Fuente string
	Status int
	Body   []byte
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("%s respondió HTTP %d", e.Fuente, e.Status)
}

// NetError es un error de red sin la URL.
type NetError struct {
	Fuente  string
	Timeout bool
	Causa   string
}

func (e *NetError) Error() string {
	if e.Timeout {
		return fmt.Sprintf("%s no respondió a tiempo; puede estar caída o lenta, reintentá en unos minutos", e.Fuente)
	}
	return fmt.Sprintf("no se pudo conectar con %s (%s)", e.Fuente, e.Causa)
}

// Retryable dice si vale la pena reintentar ante ese status.
func Retryable(status int) bool {
	return status == http.StatusTooManyRequests || status == http.StatusBadGateway ||
		status == http.StatusServiceUnavailable || status == http.StatusGatewayTimeout
}

// Get hace un GET con User-Agent, timeout por intento, reintentos con backoff ante 429/502/503/504/timeout,
// y lectura acotada a MaxBody. Respeta la cancelación de ctx.
func (c *Client) Get(ctx context.Context, f Fuente, rawURL string) ([]byte, error) {
	var lastErr error
	espera := f.Espera
	for i := 0; i < max(f.Intentos, 1); i++ {
		if i > 0 {
			if err := SleepCtx(ctx, espera); err != nil {
				return nil, err
			}
			espera *= 2
		}
		body, retry, err := c.getOnce(ctx, f, rawURL)
		if err == nil {
			return body, nil
		}
		lastErr = err
		if !retry || ctx.Err() != nil {
			break
		}
		var he *HTTPError
		if errors.As(err, &he) && he.Status == http.StatusTooManyRequests {
			espera = max(espera, 2*time.Second)
		}
	}
	if ctx.Err() != nil {
		return nil, fmt.Errorf("consulta a %s cancelada", f.Nombre)
	}
	return nil, lastErr
}

func (c *Client) getOnce(ctx context.Context, f Fuente, rawURL string) (body []byte, retry bool, err error) {
	ctx, cancel := context.WithTimeout(ctx, f.Timeout)
	defer cancel()
	r, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, false, fmt.Errorf("URL inválida para %s", f.Nombre)
	}
	r.Header.Set("User-Agent", c.UserAgent)
	r.Header.Set("Accept", "application/json")
	hc := c.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(r)
	if err != nil {
		return nil, true, ToNetError(f, err)
	}
	defer resp.Body.Close()
	body, err = io.ReadAll(io.LimitReader(resp.Body, MaxBody+1))
	if err != nil {
		return nil, true, ToNetError(f, err)
	}
	if len(body) > MaxBody {
		return nil, false, fmt.Errorf("la respuesta de %s supera %d MB; acotá la consulta", f.Nombre, MaxBody>>20)
	}
	if resp.StatusCode != http.StatusOK {
		if len(body) > 4096 {
			body = body[:4096]
		}
		if s := resp.Header.Get("Retry-After"); s != "" {
			if n, e := strconv.Atoi(s); e == nil && n > 0 && n <= 10 {
				_ = SleepCtx(ctx, time.Duration(n)*time.Second)
			}
		}
		return nil, Retryable(resp.StatusCode), &HTTPError{f.Nombre, resp.StatusCode, body}
	}
	return body, false, nil
}

// ToNetError traduce un error de transporte a uno sin URL (url.Error incluye la URL completa con el ticket).
func ToNetError(f Fuente, err error) error {
	var ne net.Error
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &ne) && ne.Timeout()) {
		return &NetError{Fuente: f.Nombre, Timeout: true}
	}
	var dnsErr *net.DNSError
	var opErr *net.OpError
	switch {
	case errors.As(err, &dnsErr):
		return &NetError{Fuente: f.Nombre, Causa: "no se resolvió el dominio"}
	case errors.As(err, &opErr):
		return &NetError{Fuente: f.Nombre, Causa: "conexión rechazada o cortada"}
	}
	return &NetError{Fuente: f.Nombre, Causa: "error de red"}
}

// SleepCtx duerme d o hasta que se cancele ctx.
func SleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

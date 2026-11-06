package httpx

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var rapida = Fuente{Nombre: "prueba", Timeout: time.Second, Intentos: 3, Espera: time.Millisecond}

func TestGetReintentaYMandaHeaders(t *testing.T) {
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") != "ua-test" || r.Header.Get("Accept") != "application/json" {
			t.Errorf("headers: %v", r.Header)
		}
		if n.Add(1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	body, err := New("ua-test").Get(context.Background(), rapida, srv.URL)
	if err != nil || string(body) != `{"ok":true}` {
		t.Fatalf("Get = %q, %v", body, err)
	}
	if n.Load() != 3 {
		t.Errorf("intentos = %d; want 3", n.Load())
	}
}

func TestGetNoReintenta4xx(t *testing.T) {
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"x"}`))
	}))
	defer srv.Close()

	_, err := New("ua").Get(context.Background(), rapida, srv.URL+"?ticket=SECRETO")
	var he *HTTPError
	if !errors.As(err, &he) || he.Status != 404 || string(he.Body) != `{"error":"x"}` {
		t.Fatalf("err = %#v", err)
	}
	if n.Load() != 1 {
		t.Errorf("un 404 no se reintenta; intentos = %d", n.Load())
	}
	if strings.Contains(err.Error(), "SECRETO") || err.Error() != "prueba respondió HTTP 404" {
		t.Errorf("mensaje: %q", err)
	}
}

func TestGetAgotaReintentos(t *testing.T) {
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()
	_, err := New("ua").Get(context.Background(), rapida, srv.URL)
	var he *HTTPError
	if !errors.As(err, &he) || he.Status != 502 || n.Load() != 3 {
		t.Fatalf("err = %v, intentos = %d", err, n.Load())
	}
}

func TestGetTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	}))
	defer srv.Close()
	f := Fuente{Nombre: "lenta", Timeout: 50 * time.Millisecond, Intentos: 2, Espera: time.Millisecond}
	_, err := New("ua").Get(context.Background(), f, srv.URL)
	var ne *NetError
	if !errors.As(err, &ne) || !ne.Timeout || !strings.Contains(err.Error(), "lenta no respondió a tiempo") {
		t.Fatalf("err = %v", err)
	}
}

func TestGetCancelado(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := New("ua").Get(ctx, Fuente{Nombre: "x", Timeout: time.Second, Intentos: 3, Espera: time.Second}, srv.URL)
	if err == nil {
		t.Fatal("con el contexto cancelado debe fallar")
	}
}

func TestGetBodyMuyGrande(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		chunk := make([]byte, 1<<20)
		for i := 0; i <= MaxBody>>20; i++ {
			_, _ = w.Write(chunk)
		}
	}))
	defer srv.Close()
	_, err := New("ua").Get(context.Background(), rapida, srv.URL)
	if err == nil || !strings.Contains(err.Error(), "supera 10 MB") {
		t.Fatalf("err = %v", err)
	}
}

func TestURLInvalidaYConexionRechazada(t *testing.T) {
	c := &Client{} // sin HTTP: usa http.DefaultClient
	if _, err := c.Get(context.Background(), rapida, "://mal"); err == nil || !strings.Contains(err.Error(), "URL inválida") {
		t.Errorf("URL inválida: %v", err)
	}
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	_, err := c.Get(context.Background(), Fuente{Nombre: "caída", Timeout: time.Second, Intentos: 1}, url+"/?ticket=SECRETO")
	var ne *NetError
	if !errors.As(err, &ne) || ne.Timeout || strings.Contains(err.Error(), "SECRETO") {
		t.Fatalf("err = %v", err)
	}
}

func TestToNetError(t *testing.T) {
	err := ToNetError(rapida, errors.New(`Get "https://x?ticket=SECRETO": boom`))
	if strings.Contains(err.Error(), "SECRETO") || strings.Contains(err.Error(), "http") {
		t.Errorf("NetError no debe incluir la URL: %s", err)
	}
	if err := ToNetError(rapida, context.DeadlineExceeded); !strings.Contains(err.Error(), "no respondió a tiempo") {
		t.Errorf("timeout: %v", err)
	}
	if !Retryable(429) || !Retryable(503) || Retryable(500) || Retryable(404) {
		t.Error("Retryable")
	}
}

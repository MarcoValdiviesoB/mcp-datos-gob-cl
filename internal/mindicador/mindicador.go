// Package mindicador obtiene indicadores económicos de mindicador.cl, con respaldo automático en findic.cl
// (misma API y mismos códigos).
package mindicador

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/MarcoValdiviesoB/mcp-datos-gob-cl/internal/httpx"
)

var (
	FuenteMindicador = httpx.Fuente{Nombre: "mindicador.cl", Timeout: 6 * time.Second, Intentos: 2, Espera: 500 * time.Millisecond}
	FuenteFindic     = httpx.Fuente{Nombre: "findic.cl", Timeout: 10 * time.Second, Intentos: 2, Espera: 500 * time.Millisecond}
)

// Validos: los que publica mindicador.cl (findic.cl usa los mismos códigos).
var Validos = []string{"uf", "ivp", "dolar", "dolar_intercambio", "euro", "ipc", "utm", "imacec", "tpm", "libra_cobre", "tasa_desempleo", "bitcoin"}

// Label es el nombre legible de cada indicador.
var Label = map[string]string{
	"uf": "UF", "ivp": "IVP", "dolar": "Dólar observado", "dolar_intercambio": "Dólar acuerdo", "euro": "Euro",
	"ipc": "IPC", "utm": "UTM", "imacec": "IMACEC", "tpm": "TPM", "libra_cobre": "Libra de cobre",
	"tasa_desempleo": "Tasa de desempleo", "bitcoin": "Bitcoin",
}

// Client consulta mindicador.cl y, si falla, findic.cl.
type Client struct {
	HTTP           *httpx.Client
	BaseURL        string // https://mindicador.cl/api
	RespaldoURL    string // https://findic.cl/api
	Fuente         httpx.Fuente
	FuenteRespaldo httpx.Fuente
}

// New crea un cliente contra mindicador.cl con respaldo en findic.cl.
func New(hc *httpx.Client) *Client {
	return &Client{HTTP: hc, BaseURL: "https://mindicador.cl/api", RespaldoURL: "https://findic.cl/api",
		Fuente: FuenteMindicador, FuenteRespaldo: FuenteFindic}
}

// Get intenta mindicador.cl y, si falla, findic.cl (misma API). Devuelve el cuerpo y la fuente usada.
func (c *Client) Get(ctx context.Context, path string) ([]byte, string, error) {
	body, err := c.HTTP.Get(ctx, c.Fuente, c.BaseURL+path)
	if err == nil {
		return body, "mindicador.cl", nil
	}
	body, err2 := c.HTTP.Get(ctx, c.FuenteRespaldo, c.RespaldoURL+path)
	if err2 == nil {
		return body, "findic.cl (respaldo: mindicador.cl no respondió)", nil
	}
	return nil, "", fmt.Errorf("no se pudieron obtener indicadores: %v; respaldo: %v. Reintentá en unos minutos", err, err2)
}

// Valor es el valor del día de un indicador.
type Valor struct {
	Codigo       string  `json:"codigo"`
	Valor        float64 `json:"valor"`
	UnidadMedida string  `json:"unidad_medida"`
	Fecha        string  `json:"fecha"`
}

// ErrDecode: la fuente respondió 200 con un JSON que no se pudo leer.
var ErrDecode = errors.New("respuesta ilegible")

// Hoy trae los valores del día de los indicadores conocidos, en el orden de Validos.
func (c *Client) Hoy(ctx context.Context) ([]Valor, string, error) {
	body, src, err := c.Get(ctx, "")
	if err != nil {
		return nil, "", err
	}
	var resp map[string]json.RawMessage
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, src, ErrDecode
	}
	var out []Valor
	for _, k := range Validos {
		var m Valor
		if raw, ok := resp[k]; !ok || json.Unmarshal(raw, &m) != nil {
			continue
		}
		m.Codigo = k
		out = append(out, m)
	}
	return out, src, nil
}

// Punto es un valor de una serie.
type Punto struct {
	Fecha string  `json:"fecha"`
	Valor float64 `json:"valor"`
}

// Serie es la serie de un indicador (últimos valores o un año).
type Serie struct {
	Nombre       string  `json:"nombre"`
	UnidadMedida string  `json:"unidad_medida"`
	Serie        []Punto `json:"serie"`
}

// Serie trae la serie de un indicador; con anio, la de ese año.
func (c *Client) Serie(ctx context.Context, indicador, anio string) (*Serie, string, error) {
	path := "/" + url.PathEscape(indicador)
	if anio != "" {
		path += "/" + url.PathEscape(anio)
	}
	body, src, err := c.Get(ctx, path)
	if err != nil {
		return nil, "", err
	}
	var resp Serie
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, src, ErrDecode
	}
	return &resp, src, nil
}

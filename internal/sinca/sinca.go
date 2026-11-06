// Package sinca trae las estaciones de calidad del aire de SINCA (Ministerio del Medio Ambiente) en tiempo real.
package sinca

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/MarcoValdiviesoB/mcp-datos-gob-cl/internal/httpx"
)

// FuenteSINCA: timeout y reintentos para SINCA.
var FuenteSINCA = httpx.Fuente{Nombre: "SINCA (MMA)", Timeout: 15 * time.Second, Intentos: 3, Espera: 500 * time.Millisecond}

// Estacion es una estación de monitoreo con sus mediciones en tiempo real.
type Estacion struct {
	Nombre   string `json:"nombre"`
	Comuna   string `json:"comuna"`
	Red      string `json:"red"`
	Region   string `json:"region"`
	Empresa  string `json:"empresa"`
	Realtime []struct {
		TableRow struct {
			Status     string   `json:"status"`
			StatusCode int      `json:"statuscode"`
			Datetime   string   `json:"datetime"`
			Parameter  string   `json:"parameter"`
			Movil      string   `json:"movil"`
			Value      float64  `json:"value"`
			Unit       string   `json:"unit"`
			Icap       *float64 `json:"icap"`
		} `json:"tableRow"`
	} `json:"realtime"`
}

// ConDatos dice si la estación tiene al menos una medición.
func (e Estacion) ConDatos() bool {
	for _, rt := range e.Realtime {
		if rt.TableRow.Parameter != "" {
			return true
		}
	}
	return false
}

// PeorEstado devuelve el estado con mayor statuscode de la estación (bueno < regular < alerta ...).
func (e Estacion) PeorEstado() (string, int) {
	st, code := "", 0
	for _, rt := range e.Realtime {
		if rt.TableRow.Parameter != "" && rt.TableRow.StatusCode >= code {
			st, code = rt.TableRow.Status, rt.TableRow.StatusCode
		}
	}
	return st, code
}

// Client consulta SINCA.
type Client struct {
	HTTP   *httpx.Client
	URL    string
	Fuente httpx.Fuente
}

// New crea un cliente contra sinca.mma.gob.cl.
func New(hc *httpx.Client) *Client {
	return &Client{HTTP: hc, URL: "https://sinca.mma.gob.cl/index.php/json/listadomapa2k19/", Fuente: FuenteSINCA}
}

// Estaciones trae todas las estaciones.
func (c *Client) Estaciones(ctx context.Context) ([]Estacion, error) {
	body, err := c.HTTP.Get(ctx, c.Fuente, c.URL)
	if err != nil {
		return nil, err
	}
	var es []Estacion
	if err := json.Unmarshal(body, &es); err != nil {
		return nil, fmt.Errorf("respuesta inesperada de SINCA: %v", err)
	}
	return es, nil
}

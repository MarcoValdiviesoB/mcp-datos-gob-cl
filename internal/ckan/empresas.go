package ckan

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// EmpresasDataset es el slug del Registro de Empresas y Sociedades.
const EmpresasDataset = "registro-de-empresas-y-sociedades"

// empresaFallback: recursos con DataStore conocidos (2026-11), por si package_show falla.
var empresaFallback = map[string]string{
	"2026": "472de7b5-384f-452d-9da5-2928689d8f2f",
	"2025": "71c8e355-226a-461e-809a-870c2275a178",
	"2024": "42ee8c8c-59cf-42e4-89af-ec19a87dbf8d",
	"2013": "fd2b91b0-eb8e-45f1-98d0-1f3316bb6468",
}

// EmpresaAnios son los recursos del Registro de Empresas por año.
type EmpresaAnios struct {
	Activos   map[string]string // año -> resource_id con DataStore
	Descargas map[string]string // año -> URL (sin DataStore)
}

var anioRe = regexp.MustCompile(`\b(19|20)\d{2}\b`)

// RecursosEmpresas descubre los recursos por año del Registro de Empresas (cache de 6 h, fallback fijo).
func (c *Client) RecursosEmpresas(ctx context.Context) *EmpresaAnios {
	c.empresaMu.Lock()
	defer c.empresaMu.Unlock()
	if c.empresaCache != nil && time.Since(c.empresaCacheT) < 6*time.Hour {
		return c.empresaCache
	}
	body, err := c.Get(ctx, "package_show", url.Values{"id": {EmpresasDataset}})
	if err == nil {
		var r struct {
			Result struct {
				Resources []Resource `json:"resources"`
			} `json:"result"`
		}
		if json.Unmarshal(body, &r) == nil && len(r.Result.Resources) > 0 {
			ea := ParseEmpresaResources(r.Result.Resources)
			if len(ea.Activos) > 0 {
				c.empresaCache, c.empresaCacheT = ea, time.Now()
				return ea
			}
		}
	}
	return &EmpresaAnios{Activos: empresaFallback, Descargas: map[string]string{}}
}

// ParseEmpresaResources clasifica los recursos por año según tengan DataStore o solo descarga.
func ParseEmpresaResources(rs []Resource) *EmpresaAnios {
	ea := &EmpresaAnios{Activos: map[string]string{}, Descargas: map[string]string{}}
	for _, r := range rs {
		y := anioRe.FindString(r.Name)
		if y == "" {
			continue
		}
		if r.DatastoreActive {
			ea.Activos[y] = r.ID
		} else {
			ea.Descargas[y] = r.URL
		}
	}
	return ea
}

// SortedYears devuelve las claves (años) de m de más nuevo a más viejo.
func SortedYears(m map[string]string) []string {
	ys := make([]string, 0, len(m))
	for y := range m {
		ys = append(ys, y)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(ys)))
	return ys
}

// RangoAnios compacta años consecutivos: [2023 ... 2014] -> "2014-2023".
func RangoAnios(ys []string) string {
	n := make([]int, 0, len(ys))
	for _, y := range ys {
		if v, err := strconv.Atoi(y); err == nil {
			n = append(n, v)
		}
	}
	sort.Ints(n)
	var parts []string
	for i := 0; i < len(n); {
		j := i
		for j+1 < len(n) && n[j+1] == n[j]+1 {
			j++
		}
		if j > i {
			parts = append(parts, fmt.Sprintf("%d-%d", n[i], n[j]))
		} else {
			parts = append(parts, strconv.Itoa(n[i]))
		}
		i = j + 1
	}
	return strings.Join(parts, ", ")
}

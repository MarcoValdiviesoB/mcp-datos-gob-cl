package text

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata" // America/Santiago aunque el sistema no tenga zoneinfo
)

// Santiago es la zona horaria de Chile continental.
var Santiago = mustLoc("America/Santiago")

func mustLoc(name string) *time.Location {
	l, err := time.LoadLocation(name)
	if err != nil {
		return time.FixedZone("CLT", -3*3600)
	}
	return l
}

// Region es una región de Chile con su número oficial y su código en Farmanet.
type Region struct {
	Num    int      // número oficial (1-16)
	Nombre string   // nombre corto
	Alias  []string // nombres alternativos y romanos, ya normalizados
	Minsal string   // fk_region de Farmanet (MINSAL), ordenado de norte a sur
}

// Regiones mapea el número oficial a fk_region de MINSAL. Construido contra getLocalesTurnos (2026-11):
// MINSAL numera de norte a sur (1=Arica ... 16=Ñuble), no con el número oficial.
var Regiones = []Region{
	{15, "Arica y Parinacota", []string{"xv", "arica"}, "1"},
	{1, "Tarapacá", []string{"i", "tarapaca"}, "2"},
	{2, "Antofagasta", []string{"ii"}, "3"},
	{3, "Atacama", []string{"iii"}, "4"},
	{4, "Coquimbo", []string{"iv"}, "5"},
	{5, "Valparaíso", []string{"v"}, "6"},
	{13, "Metropolitana", []string{"xiii", "rm", "santiago", "metropolitana de santiago", "region metropolitana"}, "7"},
	{6, "O'Higgins", []string{"vi", "libertador", "libertador general bernardo ohiggins"}, "8"},
	{7, "Maule", []string{"vii"}, "9"},
	{8, "Biobío", []string{"viii", "bio bio", "biobio"}, "10"},
	{9, "La Araucanía", []string{"ix", "araucania"}, "11"},
	{14, "Los Ríos", []string{"xiv", "rios"}, "12"},
	{10, "Los Lagos", []string{"x", "lagos"}, "13"},
	{11, "Aysén", []string{"xi", "aysen", "aisen"}, "14"},
	{12, "Magallanes", []string{"xii", "magallanes y antartica chilena"}, "15"},
	{16, "Ñuble", []string{"xvi", "nuble"}, "16"},
}

// BuscarRegion interpreta número oficial ("13"), romano ("XIII"), "RM" o nombre ("Valparaíso", "region de los rios").
func BuscarRegion(s string) (Region, bool) {
	n := NormText(s)
	n = strings.TrimPrefix(n, "region ")
	n = strings.TrimPrefix(n, "de ")
	n = strings.TrimPrefix(n, "del ")
	if n == "" {
		return Region{}, false
	}
	if num, err := strconv.Atoi(n); err == nil {
		for _, r := range Regiones {
			if r.Num == num {
				return r, true
			}
		}
		return Region{}, false
	}
	for _, r := range Regiones {
		if NormText(r.Nombre) == n {
			return r, true
		}
		for _, a := range r.Alias {
			if a == n {
				return r, true
			}
		}
	}
	for _, r := range Regiones { // coincidencia parcial: "valpo" no, pero "araucan" sí
		if len(n) >= 4 && strings.Contains(NormText(r.Nombre), n) {
			return r, true
		}
	}
	return Region{}, false
}

// ListaRegiones arma "1=Tarapacá, 2=Antofagasta, ..." para los mensajes de error.
func ListaRegiones() string {
	rs := append([]Region(nil), Regiones...)
	sort.Slice(rs, func(i, j int) bool { return rs[i].Num < rs[j].Num })
	var parts []string
	for _, r := range rs {
		parts = append(parts, fmt.Sprintf("%d=%s", r.Num, r.Nombre))
	}
	return strings.Join(parts, ", ")
}

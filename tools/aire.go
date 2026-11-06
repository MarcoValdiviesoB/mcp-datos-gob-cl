package tools

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/MarcoValdiviesoB/mcp-datos-gob-cl/internal/sinca"
	"github.com/MarcoValdiviesoB/mcp-datos-gob-cl/internal/text"
)

// ===================== Calidad del Aire (SINCA) =====================

func (h *handlers) calidadAire(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	regionIn := stringArg(req, "region")
	regionFilter := text.NormText(regionIn)
	if r, ok := text.BuscarRegion(regionIn); ok {
		regionFilter = text.NormText(r.Nombre) // "13" / "RM" / "XIII" -> "metropolitana"
	}
	comunaFilter := text.NormText(stringArg(req, "comuna"))

	estaciones, err := h.SINCA.Estaciones(ctx)
	if err != nil {
		return errResult(err), nil
	}

	var match []sinca.Estacion
	for _, e := range estaciones {
		if regionFilter != "" && !strings.Contains(text.NormText(e.Region), regionFilter) {
			continue
		}
		if comunaFilter != "" && !strings.Contains(text.NormText(e.Comuna), comunaFilter) {
			continue
		}
		match = append(match, e)
	}

	if len(match) == 0 {
		return textResult(sincaSugerencias(estaciones, regionFilter, comunaFilter)), nil
	}
	if regionFilter == "" && comunaFilter == "" {
		return textResult(sincaResumen(match)), nil
	}

	var blocks []string
	for _, e := range match {
		var b strings.Builder
		fmt.Fprintf(&b, "## %s (%s, %s)\n", text.CleanText(e.Nombre), text.CleanText(e.Comuna), text.CleanText(e.Region))
		fmt.Fprintf(&b, "Red: %s | Empresa: %s\n", text.CleanText(e.Red), text.CleanText(e.Empresa))
		if !e.ConDatos() {
			b.WriteString("- (sin datos en tiempo real)\n\n")
			blocks = append(blocks, b.String())
			continue
		}
		for _, rt := range e.Realtime {
			tr := rt.TableRow
			if tr.Parameter == "" {
				continue
			}
			icap := "-"
			if tr.Icap != nil {
				icap = strconv.FormatFloat(*tr.Icap, 'f', 0, 64)
			}
			fmt.Fprintf(&b, "- %s: **%s %s** (%s) | Estado: %s | ICAP: %s | medido: %s\n",
				text.CleanText(tr.Parameter), strconv.FormatFloat(tr.Value, 'f', -1, 64), text.CleanText(tr.Unit), text.CleanText(tr.Movil), text.OrDefault(tr.Status, "-"), icap, tr.Datetime)
		}
		b.WriteString("\n")
		blocks = append(blocks, b.String())
	}
	out, n := text.FitBlocks(blocks, text.MaxOutput-400)
	header := fmt.Sprintf("## Calidad del Aire — %d estaciones (hora de Chile)\n\n", len(match))
	if n < len(match) {
		header += fmt.Sprintf("Mostrando %d de %d; filtrá por comuna para ver el resto.\n\n", n, len(match))
	}
	return textResult(header + out), nil
}

// sincaResumen: sin filtros, una fila por región con estaciones y el peor estado.
func sincaResumen(es []sinca.Estacion) string {
	type agg struct {
		region          string
		total, sinDatos int
		peor            string
		peorCode        int
		peorEst         string
	}
	m := map[string]*agg{}
	var orden []string
	for _, e := range es {
		reg := text.CleanText(e.Region)
		a, ok := m[reg]
		if !ok {
			a = &agg{region: reg}
			m[reg] = a
			orden = append(orden, reg)
		}
		a.total++
		if !e.ConDatos() {
			a.sinDatos++
			continue
		}
		if st, code := e.PeorEstado(); code > a.peorCode {
			a.peor, a.peorCode, a.peorEst = st, code, text.CleanText(e.Nombre)+" ("+text.CleanText(e.Comuna)+")"
		}
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "## Calidad del Aire — resumen de %d estaciones\n\n", len(es))
	sb.WriteString(text.MDHeader("Región", "Estaciones", "Sin datos", "Peor estado", "Dónde"))
	for _, r := range orden {
		a := m[r]
		sb.WriteString(text.MDRow(a.region, strconv.Itoa(a.total), strconv.Itoa(a.sinDatos), text.OrDefault(a.peor, "-"), a.peorEst))
	}
	sb.WriteString("\nFiltrá con region (ej: 'Metropolitana' o '13') o comuna (ej: 'Temuco') para ver cada estación y contaminante.\n")
	return sb.String()
}

func sincaSugerencias(es []sinca.Estacion, region, comuna string) string {
	regs := map[string]bool{}
	comunas := map[string]bool{}
	for _, e := range es {
		regs[text.CleanText(e.Region)] = true
		if region == "" || strings.Contains(text.NormText(e.Region), region) {
			comunas[text.CleanText(e.Comuna)] = true
		}
	}
	keys := func(m map[string]bool) []string {
		out := make([]string, 0, len(m))
		for k := range m {
			out = append(out, k)
		}
		sort.Strings(out)
		return out
	}
	var sb strings.Builder
	sb.WriteString("No hay estaciones de SINCA para ese filtro.\n\n")
	if comuna != "" && len(comunas) > 0 {
		sb.WriteString("Comunas con estación")
		if region != "" {
			sb.WriteString(" en esa región")
		}
		sb.WriteString(": " + strings.Join(keys(comunas), ", ") + "\n\n")
	}
	sb.WriteString("Regiones válidas: " + strings.Join(keys(regs), "; ") + "\n")
	return sb.String()
}

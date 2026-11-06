package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
)

// ===================== Catálogo =====================

type catTool struct{ nombre, desc, estado string }
type catFuente struct {
	categoria string
	fuente    string
	tools     []catTool
}

var catalogo = []catFuente{
	{"Datos abiertos", "datos.gob.cl (CKAN)", []catTool{
		{"catalogo", "este índice: todo lo disponible y lo pendiente", "activo"},
		{"buscar_datasets", "busca o navega los ~3.200 datasets (q opcional, paginado, por organismo)", "activo"},
		{"categorias_datasets", "mapa por organismo/tag para navegar todo", "activo"},
		{"ver_recurso", "esquema y muestra de un recurso", "activo"},
		{"consultar", "consulta un recurso con filtros y paginación", "activo"},
		{"buscar_empresa", "Registro de Empresas y Sociedades por RUT/nombre (años con DataStore)", "activo"},
	}},
	{"Economía", "mindicador.cl (respaldo findic.cl)", []catTool{
		{"indicadores_economicos", "UF, dólar, euro, UTM, IPC, TPM, IMACEC, cobre, desempleo, bitcoin", "activo"},
	}},
	{"Sismología", "Centro Sismológico Nacional vía api.xor.cl (respaldo api.gael.cloud)", []catTool{
		{"sismos", "últimos sismos en Chile con coordenadas, hora de Chile y UTC", "activo"},
	}},
	{"Medio ambiente", "SINCA (MMA)", []catTool{
		{"calidad_aire", "MP2.5, MP10, SO2, NO2, CO en tiempo real por estación", "activo"},
	}},
	{"Salud", "Farmanet (MINSAL)", []catTool{
		{"farmacias_turno", "farmacias de turno de hoy por comuna/región", "activo"},
	}},
	{"Compras públicas", "Mercado Público / ChileCompra", []catTool{
		{"licitaciones", "licitaciones públicas por fecha/estado/código/organismo/proveedor", "ticket"},
		{"ordenes_compra", "órdenes de compra del Estado", "ticket"},
		{"proveedor_estado", "proveedor del Estado por RUT", "ticket"},
		{"organismos_publicos", "organismos compradores, filtrables por texto", "ticket"},
	}},
}

// Fuentes/áreas que aún no cubrimos (lo que nos falta).
var faltante = []string{
	"Emergencias y desastres actualizados (SENAPRED): inundaciones, alertas, damnificados. No disponible en datos.gob.cl de forma consultable.",
	"Meteorología / pronóstico (Dirección Meteorológica de Chile).",
	"Presupuesto y ejecución del gasto público (DIPRES / transparencia).",
	"Estadísticas INE detalladas (empleo, IPC por producto, censo).",
	"Series del Banco Central (más allá de los indicadores de mindicador).",
	"Transparencia: remuneraciones, viáticos, compras por organismo.",
}

func (h *handlers) catalogo(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	tieneTicket := h.MP.TieneTicket()
	var sb strings.Builder
	fmt.Fprintf(&sb, "# Catálogo — datos-gob-cl MCP (%s)\n\nFuentes y herramientas disponibles, con su estado.\n\n", h.Version)
	for _, f := range catalogo {
		fmt.Fprintf(&sb, "## %s — %s\n", f.categoria, f.fuente)
		for _, t := range f.tools {
			estado := t.estado
			switch estado {
			case "activo":
				estado = "activo ✅"
			case "ticket":
				if tieneTicket {
					estado = "configurado ✅"
				} else {
					estado = "requiere MERCADO_PUBLICO_TICKET 🔑"
				}
			default:
				estado = estado + " ⚠️"
			}
			fmt.Fprintf(&sb, "- `%s` — %s _(%s)_\n", t.nombre, t.desc, estado)
		}
		sb.WriteString("\n")
	}
	sb.WriteString("## Lo que falta / próximas fuentes\n")
	for _, m := range faltante {
		fmt.Fprintf(&sb, "- [ ] %s\n", m)
	}
	return textResult(sb.String()), nil
}

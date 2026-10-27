# mcp-datos-gob-cl

Servidor **MCP (Model Context Protocol)** en Go que le da a un agente de IA
acceso en vivo a **datos públicos de Chile**: datasets abiertos, indicadores
económicos, sismos, calidad del aire, farmacias de turno y Mercado Público.

Pensado para conectar un LLM (Claude Desktop, Cursor, Windsurf, etc.) a fuentes
oficiales chilenas y responder preguntas cruzando datos reales. Integración
independiente y no oficial.

## Herramientas

**datos.gob.cl (CKAN)**
- `buscar_datasets(q, categoria?, limite?)` — descubre qué datos existen.
- `ver_recurso(resource_id, muestra?)` — esquema + muestra de un recurso.
- `consultar(resource_id, q?, filtros?, campos?, ordenar?, limite?, offset?)` — consulta con filtros y paginación.
- `buscar_empresa(rut?, nombre?, anio?)` — Registro de Empresas y Sociedades.

**Indicadores económicos** (mindicador.cl)
- `indicadores_economicos(indicador?, anio?)` — UF, dólar, euro, UTM, IPC, TPM, IMACEC, libra de cobre, bitcoin.

**Otras fuentes**
- `sismos()` — últimos sismos en Chile (gael.cloud).
- `calidad_aire(region?, comuna?)` — MP2.5, MP10, SO2, NO2, CO en tiempo real (SINCA).
- `farmacias_turno(comuna?, region?, limite?)` — farmacias de turno (MINSAL).

**Mercado Público / ChileCompra** (requiere `MERCADO_PUBLICO_TICKET`)
- `licitaciones(fecha?, estado?, codigo?, codigo_organismo?, codigo_proveedor?)`
- `ordenes_compra(fecha?, estado?, codigo?, codigo_organismo?)`
- `proveedor_estado(rut)`
- `organismos_publicos()`

## Quick start

Requiere Go 1.26+.

```bash
go build -o mcp-datos-gob-cl .
```

Configuralo en tu cliente MCP (ej. Claude Desktop, `claude_desktop_config.json`):

```json
{
  "mcpServers": {
    "datos-gob-cl": {
      "command": "/ruta/al/mcp-datos-gob-cl",
      "env": {
        "MERCADO_PUBLICO_TICKET": "tu-ticket-opcional"
      }
    }
  }
}
```

El `MERCADO_PUBLICO_TICKET` es opcional: solo se necesita para las herramientas de
Mercado Público. El resto funciona sin credenciales.

## Fuentes de datos

- datos.gob.cl (portal de datos abiertos del Estado de Chile — CKAN)
- mindicador.cl (indicadores económicos)
- api.gael.cloud (sismos)
- SINCA — Ministerio del Medio Ambiente (calidad del aire)
- MINSAL (farmacias de turno)
- Mercado Público / ChileCompra (licitaciones y compras del Estado)

Proyecto personal, independiente y no oficial. No afiliado a los proveedores
de datos.

## Licencia

MIT.

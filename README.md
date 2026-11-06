# mcp-datos-gob-cl

Servidor **MCP (Model Context Protocol)** en Go que le da a un agente de IA
acceso en vivo a **datos públicos de Chile**: datasets abiertos, indicadores
económicos, sismos, calidad del aire, farmacias de turno y Mercado Público.

Pensado para conectar un LLM (Claude Desktop, Cursor, Windsurf, etc.) a fuentes
oficiales chilenas y responder preguntas cruzando datos reales. Integración
independiente y no oficial.

## Herramientas

> **Empezá por `catalogo`** — lista todas las fuentes y herramientas, su estado
> (activo / requiere ticket / externo) y lo que falta por cubrir. Autodescubrible
> para cualquier agente.

- `catalogo` — índice de todo lo disponible y lo pendiente.

**datos.gob.cl (CKAN)**
- `categorias_datasets(limite?)` — mapa de todo: organismos (slug y nombre) y tags con cantidad de datasets (~3.200 en total).
- `buscar_datasets(q?, organismo?, limite?, offset?)` — busca por palabra clave **o navega todos** los datasets (q vacío = todos, paginado con `offset`; filtrable por `organismo`). Muestra el slug del dataset y del organismo, y para los recursos sin DataStore su formato y URL de descarga.
- `ver_recurso(resource_id, muestra?)` — esquema + muestra de un recurso (`muestra=0` = solo esquema). Si el recurso no tiene DataStore, devuelve la URL de descarga.
- `consultar(resource_id, q?, filtros?, campos?, ordenar?, limite?, offset?)` — consulta con filtros y paginación; devuelve una tabla compacta. Los filtros comparan texto exacto (los números se convierten a string) y `ordenar` es alfabético en campos de tipo text.
- `buscar_empresa(rut?, nombre?, anio?, limite?, offset?)` — Registro de Empresas y Sociedades. El RUT se acepta con o sin puntos/guion. Sin `anio` busca en todos los años con DataStore (hoy 2013, 2024, 2025 y 2026; 2014-2023 solo existen como CSV descargable).

**Indicadores económicos** (mindicador.cl, con respaldo automático en findic.cl)
- `indicadores_economicos(indicador?, anio?)` — `uf`, `ivp`, `dolar`, `dolar_intercambio`, `euro`, `ipc`, `utm`, `imacec`, `tpm`, `libra_cobre`, `tasa_desempleo`, `bitcoin`. Con `anio` (4 dígitos) devuelve la serie de ese año.

**Otras fuentes**
- `sismos(magnitud_minima?)` — últimos sismos en Chile (Centro Sismológico Nacional vía api.xor.cl, respaldo api.gael.cloud) con coordenadas, hora de Chile y UTC.
- `calidad_aire(region?, comuna?)` — MP2.5, MP10, SO2, NO2, CO, O3 en tiempo real con hora de medición (SINCA). Sin filtros devuelve un resumen por región. Tildes y mayúsculas no importan; `region` acepta nombre o número.
- `farmacias_turno(comuna?, region?, limite?)` — farmacias de turno **de hoy** (MINSAL). `region` acepta número oficial (`13`), romano (`XIII`) o nombre.

**Mercado Público / ChileCompra** (requiere `MERCADO_PUBLICO_TICKET`)
- `licitaciones(fecha?, estado?, codigo?, codigo_organismo?, codigo_proveedor?, limite?, pagina?)` — `fecha` en `ddmmaaaa`; `estado`: activas, publicada, cerrada, desierta, adjudicada, revocada, suspendida, todos. Con `codigo` trae el detalle (organismo, monto, fechas).
- `ordenes_compra(fecha?, estado?, codigo?, codigo_organismo?, codigo_proveedor?, limite?, pagina?)` — `estado`: enviadaproveedor, aceptada, cancelada, recepcionconforme, pendienterecepcion, todos. Con `codigo` trae organismo, proveedor y montos.
- `proveedor_estado(rut)` — código de proveedor a partir del RUT (con o sin puntos).
- `organismos_publicos(q?, limite?, pagina?)` — organismos compradores con su código, filtrables por texto.

Todas las herramientas son de solo lectura. Las respuestas largas vienen paginadas y recortadas a ~20 KB con aviso de cómo seguir.

## Instalación

Requiere Go 1.23+.

```bash
go install github.com/MarcoValdiviesoB/mcp-datos-gob-cl@latest
```

El binario queda en `$(go env GOPATH)/bin/mcp-datos-gob-cl`. Para compilar desde el repo:

```bash
go build -ldflags "-X main.version=v0.2.0" -o mcp-datos-gob-cl .
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

O en Claude Code:

```bash
claude mcp add datos-gob-cl -e MERCADO_PUBLICO_TICKET=tu-ticket -- $(go env GOPATH)/bin/mcp-datos-gob-cl
```

El `MERCADO_PUBLICO_TICKET` es opcional: solo se necesita para las herramientas de
Mercado Público. El resto funciona sin credenciales.

## Tests

```bash
go vet ./... && go test ./...
```

## Fuentes de datos

- datos.gob.cl (portal de datos abiertos del Estado de Chile — CKAN)
- mindicador.cl y findic.cl (indicadores económicos)
- Centro Sismológico Nacional vía api.xor.cl, y api.gael.cloud (sismos)
- SINCA — Ministerio del Medio Ambiente (calidad del aire)
- MINSAL (farmacias de turno)
- Mercado Público / ChileCompra (licitaciones y compras del Estado)

Proyecto personal, independiente y no oficial. No afiliado a los proveedores
de datos.

## Notas de fuentes
- `farmacias_turno` usa **Farmanet (MINSAL)** directo. MINSAL numera las regiones de norte a sur; el servidor traduce el número oficial. Se descartan los locales cuyo turno no es de hoy.
- `indicadores_economicos` intenta **mindicador.cl** (timeout 6 s) y si falla usa **findic.cl** (misma API); la respuesta indica qué fuente se usó.
- Todas las llamadas tienen timeout por intento y reintentos con backoff ante 429/502/503/504 y timeouts.
- **Mercado Público** rechaza consultas simultáneas con el mismo ticket (código 10500): el servidor las serializa y reintenta. Si el ticket es compartido, pedí uno propio. El ticket nunca aparece en los mensajes de error.

## Licencia

MIT.

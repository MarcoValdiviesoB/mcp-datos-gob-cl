# Changelog

Todos los cambios relevantes de este proyecto. Formato basado en
[Keep a Changelog](https://keepachangelog.com/es-ES/1.1.0/) y versionado
[SemVer](https://semver.org/lang/es/).

## [1.0.0] - sin fecha (pendiente de release)

Primera versión estable y distribuida.

### Agregado
- 14 herramientas de solo lectura: `catalogo`, `buscar_datasets`, `categorias_datasets`,
  `ver_recurso`, `consultar`, `buscar_empresa`, `indicadores_economicos`, `sismos`,
  `calidad_aire`, `farmacias_turno`, `licitaciones`, `ordenes_compra`, `proveedor_estado`
  y `organismos_publicos`.
- Consulta de **datos** (DataStore de CKAN) y no solo de metadata: filtros exactos, búsqueda
  de texto, selección de campos, orden y paginación sobre los recursos de datos.gob.cl.
- Navegación completa de los ~3.200 datasets (búsqueda opcional, filtro por organismo, mapa
  por organismo/tag).
- Modo HTTP (`--http :8080`, streamable HTTP con `/health`) y flag `--version`.
- Distribución: binarios para macOS, Linux y Windows (amd64/arm64) con checksums vía
  GoReleaser, cask de Homebrew en `MarcoValdiviesoB/homebrew-tap`, imagen Docker multi-arch
  en `ghcr.io/marcovaldiviesob/mcp-datos-gob-cl` (distroless, usuario sin privilegios).
- `server.json` para el registro oficial de MCP (`io.github.marcovaldiviesob/mcp-datos-gob-cl`).
- Skill `skills/datos-gob-cl/SKILL.md` con flujos recomendados y trampas conocidas.

### Cambiado
- `farmacias_turno` usa Farmanet (MINSAL) directo; el recurso CKAN anterior daba 404.
- `indicadores_economicos` cae automáticamente a findic.cl si mindicador.cl no responde.
- `sismos` usa api.xor.cl (CSN) con respaldo en api.gael.cloud.
- Todas las llamadas externas tienen timeout por intento y reintentos con backoff ante
  429/502/503/504 y timeouts.

### Corregido
- Mercado Público: las consultas con el mismo ticket se serializan (evita el error 10500 por
  consultas simultáneas) y el ticket nunca aparece en los mensajes de error.
- Respuestas largas paginadas y recortadas a ~20 KB con aviso de cómo seguir.

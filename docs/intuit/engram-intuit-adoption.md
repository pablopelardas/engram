# Intuit Engram - Plan de adopcion interna

## Objetivo

Usar este fork de Engram como memoria tecnica compartida para equipos de desarrollo de Intuit, de forma que:

- los devs puedan registrar decisiones, bugs, descubrimientos y runbooks reutilizables
- los agentes trabajen con contexto historico del proyecto sin re-explorar todo siempre
- soporte y otros equipos puedan consultar una vista curada del conocimiento tecnico
- la instalacion y operacion NO interfieran con un `engram` original ya instalado por un developer

---

## Resumen ejecutivo

### Lo que ya sirve hoy

- Memoria persistente local con SQLite + FTS5.
- Recuperacion por `mem_context` y `mem_search`.
- Filtros por `type`, `project`, `scope`, `limit`.
- `topic_key` para temas duraderos.
- Plugins / setup para OpenCode, Claude Code, Gemini CLI y Codex.
- Cloud compartido con Postgres y dashboard.

### Lo que NO conviene hacer tal como esta

- Instalar este fork con el mismo nombre y la misma configuracion del `engram` original.
- Empezar con memorias totalmente locales si el objetivo final es una base compartida y canonicamente unificada.

### Recomendacion principal

Para Intuit conviene convertir este fork en un producto separado, por ejemplo `intuit-engram`, y operarlo desde el primer dia con una estrategia **cloud-first como source of truth organizacional**, manteniendo cache/local DB por ergonomia del agente pero no como autoridad principal.

Esto se desvía de la filosofia original del proyecto, que hoy es explicitamente **local-first** y considera cloud como replicacion compartida, no como autoridad central.

---

## Verificacion tecnica de la situacion actual

### 1. El original y este fork SI pueden entrar en conflicto

Si no se modifica el fork, hay varios puntos de choque:

- El data dir por default es `~/.engram` y la DB local es `engram.db`.
- Los plugins y setups registran el comando `engram`.
- El setup instala entradas MCP bajo el identificador `engram`.
- La marca del dashboard/cloud tambien sigue siendo `Engram Cloud`.

Evidencia:

- `internal/store/store.go`: default `DataDir` en `~/.engram`, DB `engram.db`.
- `docs/INSTALLATION.md`: usa `%USERPROFILE%\.engram` / `~/.engram`.
- `internal/setup/setup.go`: usa `resolveEngramCommand()` y registra el server/plugin como `engram`.
- `docs/AGENT-SETUP.md`: toda la integracion esta documentada alrededor del comando `engram`.

### Conclusión

**Renombrar solo el binario NO alcanza.**

Aunque `resolveEngramCommand()` toma la ruta real del ejecutable, el fork sigue compartiendo:

- nombre del producto
- nombre del MCP server
- nombre de las entradas de config
- data dir por defecto
- branding cloud/dashboard

Si queremos convivencia segura con el original, hay que aislar todo eso.

---

## Source of truth: recomendacion para Intuit

### Problema del enfoque local-first para este caso

El proyecto original fue disenado para:

- guardar localmente primero
- replicar luego a cloud
- tratar cloud como acceso compartido / replica

Eso funciona bien para uso personal o equipos chicos.

Para Intuit, el caso de uso cambia:

- varios devs van a aprender cosas parecidas sobre el mismo sistema
- diferentes agentes van a guardar observaciones equivalentes o casi equivalentes
- luego querremos una vista compartida, curada y confiable

### Riesgo real si empezamos local y despues unificamos

Hay dos mecanismos actuales que ayudan, pero NO resuelven el problema organizacional:

- exact dedupe en ventana local
- topic upsert por `project + scope + topic_key`

Eso no equivale a consolidacion organizacional.

Si dos personas guardan localmente observaciones equivalentes pero con distinto `sync_id`, distinto texto o distinto `topic_key`, el sistema no las va a fusionar magicamente al sincronizar. El sync es idempotente por `sync_id`, no por semantica.

### Recomendacion

Para Intuit conviene:

1. tener cloud como autoridad compartida desde el principio
2. usar la DB local como cache operacional del agente y del developer
3. definir reglas de canonizacion y curacion del lado cloud

### Implicacion arquitectonica

Esto requiere adaptar el fork porque hoy la arquitectura oficial dice:

> Local SQLite remains authoritative. Cloud is opt-in replication/shared access only.

Para Intuit recomendamos invertir esa autoridad funcional, aunque internamente siga existiendo una DB local por performance y resiliencia.

---

## Que se puede empezar a usar YA

Incluso antes de hacer todos los cambios estructurales, se puede validar valor rapidamente con:

- `mem_context` para cargar contexto reciente
- `mem_search` para buscar decisiones, bugs y patrones
- `mem_save` con contenido estructurado
- `topic_key` para temas largos
- plugins para agentes ya existentes

### Flujo minimo valido

1. El dev o agente inicia trabajo en un repo Intuit.
2. El agente llama `mem_context`.
3. Si necesita precision, llama `mem_search` con query + filtros.
4. Cuando descubre algo reutilizable, guarda una observacion estructurada.
5. Al cerrar la sesion, persiste el resumen.

### Taxonomia minima recomendada

Usar solo estos tipos al principio:

- `decision`
- `bugfix`
- `pattern`
- `config`
- `discovery`
- `runbook`
- `known_issue`

### Formato obligatorio del contenido

```md
**What**: Que se hizo o que se descubrio
**Why**: Por que importa o por que se tomo la decision
**Where**: Archivos, modulos, sistemas o rutas afectadas
**Learned**: Edge cases, restricciones, trampas o notas operativas
```

---

## Instalacion e implementacion recomendadas

## Etapa 1 - Piloto tecnico interno

### Instalacion local del fork

No usar el release publico original para Intuit. Compilar y distribuir el fork interno.

Ejemplo en Windows/macOS/Linux para usuarios tecnicos:

```bash
go install <modulo-interno>/cmd/engram@latest
```

Pero idealmente esto debe terminar como:

```bash
go install <modulo-interno>/cmd/intuit-engram@latest
```

cuando el fork ya quede aislado.

### Agentes

Una vez aislado el fork, instalar el setup del agente del mismo fork:

- OpenCode
- Claude Code
- Gemini CLI
- Codex

### Repos de Intuit

En cada repo de Intuit agregar:

```json
{
  "project_name": "nombre-canonico-del-proyecto"
}
```

en `.engram/config.json` al principio, y luego migrar esto a namespace propio del fork.

---

## Cambios de codigo necesarios en el fork

## P0 - Aislamiento total del original

Estos cambios son obligatorios para convivir con `engram` original.

### 1. Renombrar producto y binario

Propuesta:

- binario: `intuit-engram`
- MCP server id: `intuit-engram`
- branding cloud: `Intuit Engram`

### 2. Cambiar data dir por defecto

Propuesta:

- de `~/.engram`
- a `~/.intuit-engram`

o equivalente corporativo.

### 3. Cambiar variables de entorno

Propuesta:

- `ENGRAM_DATA_DIR` -> `INTUIT_ENGRAM_DATA_DIR`
- `ENGRAM_CLOUD_SERVER` -> `INTUIT_ENGRAM_CLOUD_SERVER`
- `ENGRAM_CLOUD_TOKEN` -> `INTUIT_ENGRAM_CLOUD_TOKEN`
- `ENGRAM_DATABASE_URL` -> `INTUIT_ENGRAM_DATABASE_URL`

Se puede mantener compatibilidad temporal, pero el target final debe ser namespace propio.

### 4. Cambiar nombres de config / plugin / MCP

Actualizar setup para no registrar entradas bajo `engram`.

Hay que cambiar al menos:

- nombre del MCP server en configs generadas
- nombre de plugin/entry donde aplique
- referencias de docs y scripts de setup
- branding y copy del dashboard cloud

### 5. Separar cloud.json y defaults

El cloud config local no debe compartir ruta ni naming con el original.

---

## P1 - Hacerlo util para memoria tecnica de empresa

### 1. Endurecer taxonomia

No permitir tipos arbitrarios en adopcion interna.

Lista inicial sugerida:

- `decision`
- `bugfix`
- `pattern`
- `config`
- `discovery`
- `runbook`
- `known_issue`

### 2. Agregar metadata empresarial

Campos recomendados:

- `owner_team`
- `system`
- `audience`
- `status`
- `tags`
- `severity` para incidentes / issues

### 3. Mejorar filtros de busqueda

Hoy la busqueda ya soporta:

- `type`
- `project`
- `scope`
- `limit`

Para Intuit conviene agregar:

- `owner_team`
- `system`
- `status`
- `audience`
- `tags`

### 4. Vistas curadas en dashboard

Agregar vistas para:

- runbooks
- known issues
- decisiones activas
- observaciones por sistema
- observaciones por equipo owner

---

## P2 - Cambios arquitectonicos para source of truth cloud

### 1. Replantear autoridad de escritura

Objetivo:

- cloud como verdad compartida
- local como cache / working set

Esto implica definir:

- cuando una observacion queda confirmada en cloud
- como se resuelven conflictos
- como se curan duplicados
- como se administra versionado de observaciones canonicas

### 2. Definir dedupe organizacional

El proyecto original tiene dedupe local y topic upserts.

Intuit necesita ademas:

- consolidacion por similitud semantica o reglas
- workflow de aprobacion / merge de observaciones equivalentes
- posibilidad de observacion canonica + derivadas

### 3. Definir modelo de observacion canonica

Sugerencia:

- `draft`: capturada automaticamente por agente/dev
- `reviewed`: revisada por humano
- `canonical`: referencia oficial del tema
- `deprecated`: historica, no vigente

---

## Plan de adopcion por fases

## Fase 0 - Fundacion

Objetivo: aislar el fork y fijar el protocolo de uso.

Entregables:

- rename del producto a `intuit-engram`
- data dir y env vars propias
- MCP/plugin ids propios
- taxonomia inicial
- plantilla de observacion
- 1 documento corto de buenas practicas

## Fase 1 - Piloto controlado

Objetivo: validar utilidad real en 2 o 3 repos.

Participantes:

- 4 a 6 devs
- 1 o 2 leads
- 1 agente principal por flujo

Metricas:

- observaciones guardadas por semana
- porcentaje de observaciones utiles
- cantidad de busquedas exitosas/fallidas
- casos donde el agente evito re-explorar
- ruido detectado

## Fase 2 - Cloud compartido real

Objetivo: arrancar ya con fuente compartida, no postergarla.

Entregables:

- entorno cloud de Intuit
- autenticacion corporativa o token interno controlado
- dashboard visible para devs/leads
- reglas de curacion semanales

## Fase 3 - Curacion y soporte

Objetivo: abrir subconjuntos utiles a otros equipos.

Entregables:

- vista de `runbook`
- vista de `known_issue`
- filtros por sistema / owner team
- guia de consulta para soporte

---

## Politica de uso recomendada

## Que SI guardar

- decisiones tecnicas que siguen vigentes
- bugs no triviales y su causa raiz
- patrones de implementacion repetibles
- configuraciones peligrosas o sensibles
- descubrimientos del codigo que ahorran tiempo futuro
- runbooks operativos
- issues conocidos con workaround

## Que NO guardar

- cada comando ejecutado
- exploracion menor
- pruebas descartadas sin valor futuro
- comentarios vagos sin contexto
- observaciones duplicadas sin `topic_key` ni titulo claro

## Convencion de titulos

Usar formato corto y buscable:

- `auth: refresh token race`
- `sync: chunk import duplication`
- `dashboard: htmx partial contract`
- `runbook: cloud rollback procedure`
- `known-issue: sandbox login timeout`

## Convencion de topic_key

- `architecture/auth`
- `bugfix/sync-import`
- `runbook/cloud-rollback`
- `known-issue/login-timeout`

---

## Embeddings / vector DB

## Recomendacion actual

No empezar por embeddings.

Primero validar:

- disciplina de escritura
- taxonomia
- metadata
- filtros
- valor real del retrieval lexical

## Cuando si evaluarlo

Cuando aparezcan consultas como:

- "no recuerdo el termino exacto, pero era algo parecido a..."
- "hubo un problema similar en otro proyecto"
- "quiero encontrar incidentes conceptualmente relacionados"

## Estrategia correcta si llega ese momento

Busqueda hibrida:

- FTS para precision lexical
- embeddings para recall semantico

No reemplazar FTS5; complementarlo.

---

## Decisiones recomendadas

### Decision 1

Este fork debe vivir como producto separado de `engram` original.

### Decision 2

Para Intuit conviene cloud como source of truth compartido desde el principio.

### Decision 3

La DB local debe existir, pero como cache operacional y superficie de trabajo del agente, no como autoridad organizacional.

### Decision 4

Antes de embeddings, primero hay que resolver naming, taxonomia, metadata, curacion y filtros.

---

## Backlog inicial sugerido

## P0

- renombrar binario / branding / MCP server id
- cambiar data dir default
- cambiar env vars y cloud config namespace
- cambiar setup/plugins para no registrar `engram`
- documentar instalacion interna

## P1

- taxonomia cerrada
- metadata extra en observaciones
- filtros nuevos de busqueda
- vistas de dashboard para runbooks / known issues / decisiones

## P2

- cloud-authoritative workflow
- dedupe organizacional
- canonizacion de observaciones
- apertura controlada a soporte

## P3

- retrieval hibrido con embeddings
- relaciones cross-project mas ricas
- integraciones con tickets / incidentes / PRs

---

## Siguiente paso recomendado

Implementar primero el bloque P0 de aislamiento del fork. Sin eso, cualquier piloto corre el riesgo de mezclarse con el `engram` original del developer y generar una adopcion confusa.

---

## Estado actual del P0 en este repo

### Ya implementado en este corte

- namespace de producto interno: `intuit-engram`
- data dir default: `~/.intuit-engram`
- DB local default: `intuit-engram.db`
- cloud config local: `intuit-cloud.json`
- MCP server name: `intuit-engram`
- env vars nuevas soportadas para runtime principal:
  - `INTUIT_ENGRAM_DATA_DIR`
  - `INTUIT_ENGRAM_CLOUD_SERVER`
  - `INTUIT_ENGRAM_CLOUD_TOKEN`
  - `INTUIT_ENGRAM_CLOUD_AUTOSYNC`
  - `INTUIT_ENGRAM_DATABASE_URL`
  - `INTUIT_ENGRAM_CLOUD_ADMIN`
  - `INTUIT_ENGRAM_JWT_SECRET`
- compatibilidad temporal con las env vars legacy `ENGRAM_*`
- setup de MCP para OpenCode / Gemini / Claude user MCP con id `intuit-engram`

### Pendiente para completar P0

- reemplazar branding y copy visible que todavia dice `engram` / `Engram Cloud`
- ajustar plugin marketplace / nombre de plugin de Claude Code para que no dependa del plugin publico original
- revisar scripts y plugins embebidos que todavia tienen strings legacy
- revisar tests y docs publicas que siguen asumiendo `engram`, `cloud.json`, `engram.db`, `.engram`
- decidir si tambien se renombra el folder de sync del repo (`.engram/`) o si se mantiene por compatibilidad operativa

### Decision provisoria sobre sync folder

Por ahora conviene **mantener `.engram/`** hasta definir migracion completa de tooling y plugins. Cambiar data dir local y MCP/runtime ya reduce el conflicto principal con una instalacion personal del `engram` original. El rename del folder de sync del repo es una segunda etapa porque impacta:

- import/export de chunks
- scripts de plugins
- docs de operacion
- potencial compatibilidad con repos que ya tengan historial `.engram/`

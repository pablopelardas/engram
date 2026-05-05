# Intuit Engram

Memoria técnica persistente para equipos de desarrollo de Intuit.

## Qué es

Intuit Engram es un fork interno de [Engram](https://github.com/Gentleman-Programming/engram) adaptado para operar como producto corporativo aislado del original. Permite a los agentes de IA y desarrolladores guardar decisiones, bugs, descubrimientos y runbooks en una base de conocimiento compartida.

## Instalación

```bash
# Compilar desde este repo
go build -o intuit-engram ./cmd/intuit-engram

# O instalar directamente
go install ./cmd/intuit-engram
```

## Configuración rápida

### 1. Data directory

Por defecto usa `~/.intuit-engram/` (aislado de `~/.engram` del original).

Variables de entorno:
- `INTUIT_ENGRAM_DATA_DIR` — override del data dir
- `ENGRAM_DATA_DIR` — fallback legacy para compatibilidad temporal

### 2. Config de repo

En cada repo de Intuit agregar `.intuit-engram/config.json`:

```json
{
  "project_name": "nombre-canonico-del-proyecto"
}
```

### 3. Setup de agentes

Soporta 4 agentes MCP:

```bash
intuit-engram setup opencode
intuit-engram setup claude-code
intuit-engram setup gemini-cli
intuit-engram setup codex
```

Ver [docs/AGENT-SETUP.md](docs/AGENT-SETUP.md) para detalles por agente.

### 4. Cloud (opcional)

```bash
intuit-engram cloud config --server https://tu-cloud-interno.intuit.com
intuit-engram cloud enroll <project-name>
```

## Uso básico

```bash
# Guardar una observación
intuit-engram save "auth: race condition en refresh token" "Descubrimiento sobre el bug" --type bugfix --project mi-proyecto

# Buscar
intuit-engram search "refresh token race"

# Contexto reciente
intuit-engram context mi-proyecto

# Stats
intuit-engram stats
```

## Taxonomía recomendada

Tipos de observación para uso interno:

- `decision` — decisiones técnicas vigentes
- `bugfix` — bugs no triviales y causa raíz
- `pattern` — patrones de implementación repetibles
- `config` — configuraciones peligrosas o sensibles
- `discovery` — descubrimientos que ahorran tiempo
- `runbook` — procedimientos operativos
- `known_issue` — issues conocidos con workaround

## Arquitectura

- **Local**: SQLite + FTS5 en `~/.intuit-engram/intuit-engram.db`
- **Cloud**: Postgres opcional para sync compartido
- **MCP**: stdio transport para cualquier agente compatible

Ver [docs/intuit/engram-intuit-adoption.md](docs/intuit/engram-intuit-adoption.md) para el plan completo de adopción.

## Namespace

| Aspecto | Valor |
|---------|-------|
| Binario | `intuit-engram` |
| Data dir | `~/.intuit-engram` |
| DB | `intuit-engram.db` |
| Cloud config | `intuit-cloud.json` |
| Repo config dir | `.intuit-engram` |
| MCP server ID | `intuit-engram` |
| Env vars | `INTUIT_ENGRAM_*` (con fallback `ENGRAM_*`) |

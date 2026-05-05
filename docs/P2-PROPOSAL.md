# Propuesta P2: Cloud Source of Truth + Curación

## Contexto del entorno

- **Servidores**: Windows Server con SQL Server
- **Equipos**: 5 grandes + subdivisiones, desarrollo conjunto
- **Volumen**: Alto — muchas aplicaciones, observaciones frecuentes
- **Curadores**: Devs y leads, en momentos separados vía web
- **Futuro**: Posible migración a PostgreSQL en Linux

---

## Fase P2.1: Modelo de observación canónica (DB local)

### Estados de observación

```sql
ALTER TABLE observations ADD COLUMN canonical_status TEXT 
    NOT NULL DEFAULT 'draft' 
    CHECK (canonical_status IN ('draft', 'reviewed', 'canonical', 'deprecated'));
```

| Estado | Visible para | Transición |
|--------|--------------|------------|
| `draft` | Autor + owner_team | auto al guardar |
| `reviewed` | Equipo | lead/dev aprueba |
| `canonical` | Toda la org | lead eleva |
| `deprecated` | Toda la org (marcada) | lead/dev depreca |

### Reglas de visibilidad

- `draft`: solo aparece en búsquedas del autor y en dashboard de curación pendiente
- `reviewed`: visible para el equipo (`owner_team`)
- `canonical`: visible para todos los proyectos
- `deprecated`: aparece marcada, puede filtrarse

### MCP tools (agente)

- `mem_save` — guarda SIEMPRE como `draft`. El agente no decide estado, solo guarda.

### Dashboard web (curación)

- **Aprobar** (→ `reviewed`) — lead/dev valida que es correcto
- **Elevar** (→ `canonical`) — lead marca como referencia oficial
- **Deprecar** (→ `deprecated`) — marca como histórico, no vigente
- **Rechazar** (borrar) — elimina el draft

### Campos adicionales

- `submitted_at` — cuando se guardó como draft
- `reviewed_at` — cuando se revisó
- `reviewed_by` — quién revisó
- `promoted_at` — cuando se elevó a canonical
- `promoted_by` — quién elevó

---

## Fase P2.2: Flujo de curación web

### Dashboard de curación (`/dashboard/curation`)

**Vista "Pendientes"** — para leads/devs curadores:
- Observaciones en `draft` del equipo
- Filtros: por proyecto, por tipo, por antigüedad
- Acciones: Aprobar (→ `reviewed`), Elevar (→ `canonical`), Rechazar (→ `deprecated`)

**Vista "Canónicas del equipo"**:
- Observaciones `canonical` del `owner_team`
- Acciones: Deprecar, Editar

**Vista "Mis borradores"** — para devs:
- Propios `draft` sin revisar
- Acciones: Editar, Eliminar, Solicitar revisión

### Notificaciones

- Email/slack semanal a leads con count de pendientes
- Badge en dashboard cuando hay borradores del equipo sin revisar

### Permisos

| Rol | Puede ver | Puede editar |
|-----|-----------|--------------|
| Dev (autor) | Sus drafts + reviewed/canonical del equipo | Sus drafts |
| Lead | Todo del equipo + canonical org-wide | Todo del equipo |
| Admin | Todo | Todo |

---

## Fase P2.3: Cloud sync con SQL Server

### Estrategia: Adaptador SQL Server

En lugar de reescribir todo el cloudstore, crear un **adaptador**:

```go
// internal/cloud/sqlserverstore/
type SQLServerStore struct {
    db *sql.DB
}
```

**Tablas en SQL Server** (equivalentes a PostgreSQL):

```sql
-- Chunks
CREATE TABLE cloud_chunks (
    id INT IDENTITY(1,1) PRIMARY KEY,
    chunk_id NVARCHAR(255) NOT NULL UNIQUE,
    project_name NVARCHAR(255) NOT NULL,
    created_by NVARCHAR(255),
    client_created_at DATETIME2,
    sessions_count INT DEFAULT 0,
    observations_count INT DEFAULT 0,
    prompts_count INT DEFAULT 0,
    payload NVARCHAR(MAX),
    created_at DATETIME2 DEFAULT GETDATE()
);

-- Mutations
CREATE TABLE cloud_mutations (
    id INT IDENTITY(1,1) PRIMARY KEY,
    project NVARCHAR(255) NOT NULL,
    entity NVARCHAR(50) NOT NULL,
    entity_key NVARCHAR(255) NOT NULL,
    op NVARCHAR(20) NOT NULL,
    payload NVARCHAR(MAX),
    created_at DATETIME2 DEFAULT GETDATE()
);

-- Users
CREATE TABLE cloud_users (
    id UNIQUEIDENTIFIER DEFAULT NEWID() PRIMARY KEY,
    username NVARCHAR(255) NOT NULL UNIQUE,
    email NVARCHAR(255),
    password_hash NVARCHAR(255),
    created_at DATETIME2 DEFAULT GETDATE()
);
```

### Sync workflow con cloud source of truth

```
1. Dev guarda observación → local DB como 'draft'
2. Local sync push → cloud mutation
3. Cloud aplica mutation → chunk
4. Lead cura en dashboard → cambia a 'reviewed'/'canonical'
5. Cloud genera nueva mutation de estado
6. Otros devs sync pull → reciben estado actualizado
7. Local cache actualiza canonical_status
```

### Conflict resolution

- **Mismo contenido, diferente estado**: gana el estado más restrictivo
- **Edición concurrente**: último en modificar gana (con timestamp)
- **Observaciones duplicadas**: workflow de merge propuesto

---

## Fase P2.4: Dedupe organizacional

### Detección de duplicados

- **FTS5** para búsqueda por título/contenido similar
- **Embedding** (futuro) para similitud semántica

### Workflow de merge

1. Sistema detecta 2+ observaciones similares (score > 0.8)
2. Marca como `pending_merge` en tabla `merge_candidates`
3. Dashboard muestra "Observaciones para consolidar"
4. Lead selecciona:
   - **Merge**: crear observación canónica combinada, deprecar originales
   - **Keep separate**: marcar como `not_duplicate`
   - **Link**: relacionar como `related` sin merge

---

## Roadmap propuesto

| Fase | Entregable | Tiempo estimado |
|------|-----------|----------------|
| P2.1 | Estados canónicos + MCP tools | 1-2 semanas |
| P2.2 | Dashboard de curación | 2-3 semanas |
| P2.3 | Cloud sync SQL Server | 3-4 semanas |
| P2.4 | Dedupe org + merge workflow | 2-3 semanas |

**Total**: 8-12 semanas

---

## Decisiones técnicas pendientes

1. **¿Adaptador SQL Server o rewrite?**
   - Adaptador: menos riesgo, más rápido
   - Rewrite: más limpio, pero más trabajo

2. **¿Auth corporativa (Active Directory) o tokens?**
   - AD: integración con infraestructura existente
   - Tokens: más simple, pero gestión manual

3. **¿Deploy del cloud server?**
   - Windows Server + servicio
   - Docker en Windows (si tienen)
   - Esperar Linux + PostgreSQL

¿Querés que profundice en alguna fase específica o que empecemos con SDD formal?

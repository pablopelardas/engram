# Engram x IntuitHub - Integracion para visibilidad organizacional

## Contexto

Este documento define la integracion entre **Engram** (memoria tecnica persistente) e **IntuitHub** (gestion de equipos, colaboradores y aplicaciones de Intuit).

Se asume que el lector conoce:
- el modelo de 3 estadios de Engram (`draft -> reviewed -> canonical`, ver `docs/P2-PROPOSAL.md`)
- el modelo de IntuitHub (`Collaborators`, `Teams`, `Applications`, `TeamApplications`, `CollaboratorTeams`)

---

## Objetivo

Que las observaciones de Engram tengan visibilidad organizacional correcta:

| Estadio | Quien la ve |
|---|---|
| `draft` | solo el autor |
| `reviewed` | colaboradores de algun team owner de la app |
| `canonical` | toda la organizacion |
| `deprecated` | toda la organizacion (con badge) |

Sin duplicar el modelo de identidad y ownership: **IntuitHub es source of truth** de quien es quien y de que team owns que app. Engram cachea lo necesario para hacer filtros eficientes.

---

## Decisiones tomadas

### D1. Curacion abierta a todos los devs (no solo leads)

El dashboard de curacion `/dashboard/curation` deja de ser "panel de leads" y pasa a ser **panel de cada dev** con vistas distintas segun rol:

- **"Mis drafts"** (todos los devs) - sus propios drafts. Acciones: Edit, Promote-to-reviewed, Reject.
- **"Pendientes del equipo"** (solo si el dev es Maintainer/Lead de algun team owner) - drafts y reviewed de las apps que su team owns. Acciones: Promote, Deprecate, Reject.
- **"Canonicas"** (todos, read-only) - observaciones canonical org-wide.

### D2. Apps con multiples team owners

`TeamApplications` en IntuitHub es M:N. Una app puede tener varios teams owners.

Implicacion: Engram **no guarda** `owner_team` en la observacion. El team owner se **deriva** de `project` cruzando contra una tabla mirror `app_team_ownership` cacheada desde IntuitHub.

### D3. Sync por pull desde Engram hacia IntuitHub

El job de sync corre dentro de Engram-cloud (pull). Cada 15 min consulta IntuitHub via API key admin para hidratar:
- `cloud_users` (mirror de `Collaborators` + sus memberships)
- `app_team_ownership` (mirror de `TeamApplications`)

Tambien disparable manualmente: `engram cloud sync intuit`.

### D4. Proyectos no registrados en IntuitHub quedan personales

Si una observacion tiene un `project` que NO matchea ninguna `Applications.ApplicationName` en IntuitHub:
- queda visible solo al autor (como si fuera draft eterno)
- nunca puede llegar a reviewed/canonical
- es feature, no bug: fuerza a registrar apps en IntuitHub antes de canonizar conocimiento

Caso valvula de escape: `scope = 'personal'` exceptua del filtro de teams. Las personales son siempre privadas del dev sin importar el estado.

### D5. App key admin para sync

Engram-cloud necesita una API key de IntuitHub con `Role = Admin` para llamar a los endpoints de sync sin filtro de visibilidad. Se crea un colaborador especial `engram-cloud-sync@intuit` con esa key, almacenada en la config de Engram-cloud (env var `INTUIT_ENGRAM_INTUITHUB_ADMIN_KEY`).

---

## Arquitectura

```
+---------------------+         pull cada 15 min        +---------------------+
|   IntuitHub API     | <-------------------------------|   Engram Cloud      |
|   (source of truth) |  GET /api/teams                 |                     |
|                     |  GET /api/teamapplications      |  cloud_users        |
|                     |  GET /api/collaborators         |  app_team_ownership |
+---------------------+                                  +---------------------+
         ^                                                        ^
         |                                                        |
         | X-API-Key (dev)                                        | mem_save / mem_search
         | GET /api/auth/me                                       | (auth via cached cloud_user)
         |                                                        |
         |                                                        |
+--------+-----------+                                  +---------+----------+
| dev local /        |  ------ same X-API-Key -------> | dev local /         |
| browser            |                                  | agent (MCP)         |
+--------------------+                                  +---------------------+
```

El dev usa **una sola API key** de IntuitHub. Esa misma key sirve para autenticarse en Engram-cloud — el middleware de Engram delega la validacion a IntuitHub.

---

## Endpoints de IntuitHub disponibles (verificados)

| Endpoint | Auth requerida | Filtro de visibilidad | Uso desde Engram |
|---|---|---|---|
| `GET /api/auth/me` | X-API-Key | n/a | resolver dev autenticado + sus teams + apps (lazy hydration de `cloud_users`) |
| `GET /api/teams` | publico | `TeamVisibilityService` aplicado | sync de teams - **necesita admin key para ver todos** |
| `GET /api/teamapplications` | publico | sin filtro | sync de ownership (entrega todas las asignaciones activas) |
| `GET /api/applications` | publico | (revisar) | catalogo de apps |
| `GET /api/collaborators` | Admin/Editor/Viewer | `TeamVisibilityService` aplicado | sync masivo de colaboradores con admin key. Trae `Teams` embebidos con `TeamCode` y `Role` per-team |
| `GET /api/collaborators/{id}` | Admin/Editor/Viewer | sin filtro adicional | refrescar un colaborador puntual |

Endpoints confirmados leyendo el codigo:

- `GET /api/teamapplications` retorna todas las asignaciones activas sin filtro de visibilidad.
- `GET /api/teams` aplica `TeamVisibilityService` - por eso necesita admin key para sync completo.
- `GET /api/collaborators` aplica `TeamVisibilityService` tambien - admin key ve todos los colaboradores con sus teams y roles per-team. Solo Admin ve `ApiKey` en el response (Engram no la necesita; solo email/role/teams).

---

## Schema de Engram Cloud

### Tablas nuevas

```sql
-- Mirror cacheado de IntuitHub.Collaborators + memberships
CREATE TABLE cloud_users (
    id                       INT PRIMARY KEY IDENTITY(1,1),
    intuit_collaborator_id   INT NOT NULL UNIQUE,
    email                    NVARCHAR(255) NOT NULL,
    full_name                NVARCHAR(200),
    role                     NVARCHAR(20),         -- 'Admin' | 'Editor' | 'Viewer' | 'None'
    team_codes_json          NVARCHAR(MAX),        -- JSON array: ["BACKEND","DEVOPS"]
    memberships_json         NVARCHAR(MAX),        -- JSON: [{team_code, role_in_team, apps:[{app_name, role_in_app}]}]
    has_full_visibility      BIT NOT NULL DEFAULT 0,  -- true si CUALQUIER team del dev tiene HasFullVisibility=1
    cached_at                DATETIME2 NOT NULL DEFAULT GETDATE()
);

-- Mirror cacheado de IntuitHub.TeamApplications (read-only)
CREATE TABLE app_team_ownership (
    application_name NVARCHAR(100) NOT NULL,  -- = IntuitHub.Applications.ApplicationName
    team_code        NVARCHAR(20)  NOT NULL,  -- = IntuitHub.Teams.TeamCode
    role             NVARCHAR(50)  NULL,      -- 'Maintainer' | 'Contributor' | etc
    synced_at        DATETIME2     NOT NULL DEFAULT GETDATE(),
    PRIMARY KEY (application_name, team_code)
);

-- Estado del job de sync
CREATE TABLE intuithub_sync_state (
    id                  INT PRIMARY KEY DEFAULT 1,
    last_sync_at        DATETIME2 NULL,
    last_sync_status    NVARCHAR(20) NULL,  -- 'success' | 'failure' | 'partial'
    last_sync_error     NVARCHAR(MAX) NULL,
    teams_synced        INT NULL,
    collaborators_synced INT NULL,
    ownerships_synced   INT NULL,
    CONSTRAINT CK_intuithub_sync_state_singleton CHECK (id = 1)
);
```

### Cambios en `observations`

```sql
-- created_by ahora apunta a IntuitHub.Collaborators
ALTER TABLE observations
    ADD created_by_collab_id INT NULL;

-- owner_team se va: el team owner se deriva de project + app_team_ownership
ALTER TABLE observations
    DROP COLUMN owner_team;
```

**Migracion de datos existentes**: las observaciones actuales con `owner_team` string libre se migran best-effort:
- `created_by` (string) -> match contra `cloud_users.email` -> setea `created_by_collab_id`
- si no matchea: queda NULL y la observacion se marca como `scope='personal'`
- `owner_team` se descarta (la nueva derivacion lo reemplaza)

---

## Reglas de visibilidad (SQL pseudocodigo)

```sql
-- Variables de sesion (resueltas por middleware)
DECLARE @cur_user_collab_id INT;
DECLARE @cur_user_team_codes_json NVARCHAR(MAX);
DECLARE @cur_user_has_full_visibility BIT;

DECLARE @my_teams TABLE (team_code NVARCHAR(20));
INSERT INTO @my_teams SELECT value FROM OPENJSON(@cur_user_team_codes_json);

SELECT o.*
FROM observations o
WHERE o.deleted_at IS NULL
  AND (
    -- Personal: visible solo al autor, sin importar status
    (o.scope = 'personal' AND o.created_by_collab_id = @cur_user_collab_id)

    -- Draft: solo si soy el autor
    OR (o.canonical_status = 'draft' AND o.created_by_collab_id = @cur_user_collab_id)

    -- Reviewed: si soy de algun team owner, o tengo full visibility
    OR (o.canonical_status = 'reviewed' AND (
         @cur_user_has_full_visibility = 1
         OR EXISTS (
            SELECT 1 FROM app_team_ownership ato
            WHERE ato.application_name = o.project
              AND ato.team_code IN (SELECT team_code FROM @my_teams)
         )
       ))

    -- Canonical: todos
    OR o.canonical_status = 'canonical'

    -- Deprecated: todos (UI los muestra con badge)
    OR o.canonical_status = 'deprecated'
  );
```

---

## Reglas de permisos para curacion

### Niveles de rol (hay 3 en IntuitHub, importantes los niveles 1 y 3)

1. **`Collaborators.Role`** - rol global del dev en la org. {Admin, Editor, Viewer, None}
2. `CollaboratorTeams.Role` - rol del dev DENTRO de un team. {Member, Lead, ...}
3. **`TeamApplications.Role`** - rol del team SOBRE una app. {Maintainer, Contributor, ...}

### Permisos por accion

```go
func canPromoteToReviewed(user, obs) bool {
    // El autor puede promover su propio draft
    if obs.CanonicalStatus == "draft" && obs.CreatedByCollabID == user.CollabID {
        return true
    }
    // Maintainer/Lead de un team owner de la app
    if isMaintainerOfApp(user, obs.Project) {
        return true
    }
    return false
}

func canPromoteToCanonical(user, obs) bool {
    if user.Role == "Admin" {
        return true
    }
    // Lead de team owner con role Maintainer sobre la app
    if isMaintainerOfApp(user, obs.Project) && hasLeadInOwnerTeam(user, obs.Project) {
        return true
    }
    return false
}

func canRejectDraft(user, obs) bool {
    // Solo el autor o un Admin puede borrar fisicamente
    return obs.CreatedByCollabID == user.CollabID || user.Role == "Admin"
}

func canDeprecate(user, obs) bool {
    if user.Role == "Admin" {
        return true
    }
    return isMaintainerOfApp(user, obs.Project)
}

// Helpers
func isMaintainerOfApp(user, project) bool {
    // Recorre user.MembershipsJSON: existe team con role_in_team in {Lead, Maintainer}
    // Y ese team tiene role_in_app == 'Maintainer' sobre `project`
    ...
}
```

---

## Sprints

### Sprint 1 - Auth bridge + sync de teams (5-7 dias)

**Objetivo**: que un dev pueda autenticarse en Engram-cloud con su API key de IntuitHub y que las tablas mirror se hidraten.

Entregables:

1. Middleware `IntuitHubAuth` en Engram Cloud (Go):
   - lee header `X-API-Key`
   - cache LRU en memoria de tokens validados (TTL 5 min)
   - en miss, llama `GET /api/auth/me` a IntuitHub
   - parsea respuesta y persiste/actualiza `cloud_users`
   - inyecta `cloud_user` en el request context
2. Tablas `cloud_users`, `app_team_ownership`, `intuithub_sync_state` (migracion).
3. Job de sync (`internal/intuithub/sync.go`):
   - cron interno cada 15 min
   - tambien expone CLI: `engram cloud sync intuit`
   - usa `INTUIT_ENGRAM_INTUITHUB_ADMIN_KEY` para llamadas admin
   - hidrata:
     - `cloud_users` desde `GET /api/collaborators` (admin key bypassa filtro de visibilidad y trae Teams embebidos con role per-team)
     - `app_team_ownership` desde `GET /api/teamapplications` + lookup de `TeamCode` y `ApplicationName` via `GET /api/teams` y `GET /api/applications`
   - lazy hydration adicional en el middleware `IntuitHubAuth`: si un dev hace login y no esta en `cloud_users` (ej. nuevo colaborador entre syncs), se hidrata desde `GET /api/auth/me` sobre la marcha
   - actualiza `intuithub_sync_state`
4. Config: `INTUIT_ENGRAM_INTUITHUB_BASE_URL`, `INTUIT_ENGRAM_INTUITHUB_ADMIN_KEY`.

**Definition of done**:
- 3 devs en 3 teams diferentes pueden hacer login con su API key.
- `engram cloud doctor` muestra estado de sync OK.
- `cloud_users` y `app_team_ownership` se rehidratan correctamente.

### Sprint 2 - Filtros de visibilidad por estadio (4-5 dias)

**Objetivo**: que mem_search/pull aplique las reglas de visibilidad correctamente.

Entregables:

1. Refactor de `internal/store` y endpoints de pull para aplicar el WHERE compuesto de la seccion de visibilidad.
2. `mem_save`:
   - setea `created_by_collab_id` desde la sesion autenticada (no mas string libre)
   - elimina parametro `owner_team` (deprecated, retorna warning si se pasa)
3. Migracion de observaciones existentes (best-effort match de `created_by` -> `cloud_users.email`).
4. Tests e2e:
   - dev A (team backend) guarda draft - dev B (team backend) NO lo ve
   - dev A promote-to-reviewed - dev B (team backend) lo ve, dev C (team frontend) no
   - app con 2 team owners: cualquier miembro de cualquiera de los 2 teams ve los reviewed
   - dev con `HasFullVisibility=1` ve todos los reviewed sin importar team
   - observacion con `scope=personal` solo la ve el autor

**Definition of done**:
- Tests pasan.
- `mem_search` desde 3 devs distintos retorna sets disjuntos correctos.

### Sprint 3 - Curacion con permisos por rol (3-4 dias)

**Objetivo**: dashboard de curacion abierto a todos los devs con vistas distintas por rol.

Entregables:

1. Refactor de `/dashboard/curation`:
   - tab **"Mis drafts"** - siempre visible, scope = `created_by_collab_id = me`
   - tab **"Pendientes del equipo"** - visible solo si soy Maintainer/Lead de algun team owner
   - tab **"Canonicas"** - read-only, todos
2. Handlers con permisos chequeados (las funciones de la seccion de permisos).
3. Boton **"Reject"** (delete fisico) - drop del row + mutation `delete` al cloud sync queue.
4. Boton **"Promote to reviewed"** disponible en "Mis drafts" para el autor.
5. Boton **"Promote to canonical"** disponible solo para Admin o Lead-Maintainer.
6. Auth del dashboard via IntuitHub middleware (mismo de Sprint 1).

**Definition of done**:
- 3 perfiles (dev regular, lead de team, admin) ven y pueden hacer cosas distintas.
- Eliminar un draft propio borra fisicamente el row.

### Sprint 4 - Pestaña "Memoria" en IntuitHub UI (1 semana, opcional)

**Objetivo**: que el dev pueda ver/buscar memoria desde IntuitHub sin abrir Engram-dashboard separado.

Entregables:

1. En `intuit-hub/src/pages/ApplicationDetailPage.vue`: tab "Memoria" que llama a `engram-cloud/api/observations?project={app_name}`.
2. Service en `intuit-hub/src/services/engram.service.ts` que reusa la API key del dev autenticado.
3. Botones `promote` / `deprecate` solo si el dev tiene permisos correspondientes.
4. Busqueda basica por titulo/contenido.

**Coordinacion necesaria**: equipo de IntuitHub debe absorber este sprint. Se puede postergar — los Sprints 1-3 funcionan sin esto.

---

## Riesgos y mitigaciones

| Riesgo | Mitigacion |
|---|---|
| IntuitHub cae - dev no puede autenticarse en Engram | Cache LRU de 5 min en middleware permite operar offline brevemente; despues falla limpio |
| Sync de `app_team_ownership` falla y queda stale | `intuithub_sync_state` registra `last_sync_at`; alertar si > 1h sin sync; `engram cloud doctor` lo expone |
| Observaciones con `project` que ya no existe en IntuitHub | Quedan visibles solo al autor (mismo trato que personales). No se pierde data |
| Migracion: observaciones viejas con `created_by` que no matchea email | Quedan con `created_by_collab_id = NULL` y `scope = personal`; el autor las puede recuperar via UI manual |
| Team renombra su `TeamCode` en IntuitHub | El sync detecta el cambio y actualiza `app_team_ownership`; las observaciones referencian `project` (nombre app), no team_code, asi que no rompen |
| App renombra su `ApplicationName` | Esto SI es problematico - rompe el join `observations.project` <-> `app_team_ownership.application_name`. Mitigacion: agregar `application_id` (estable) a `app_team_ownership` y a `observations` en una fase futura |

---

## Decisiones cerradas (resumen)

1. **Curacion**: abierta a todos. Vistas distintas por rol. Confirmado.
2. **Apps con multiples teams owners**: mirror cacheado `app_team_ownership` (no se duplica `owner_team` en observation). Confirmado.
3. **Endpoint para sync**: `GET /api/teamapplications` ya existe. Confirmado.
4. **App key admin**: se crea colaborador `engram-cloud-sync@intuit` con `Role = Admin`, key en env var. Confirmado.
5. **Proyectos no registrados**: quedan personales del autor, no escapan. Confirmado.
6. **Sync direction**: pull desde Engram-cloud cada 15 min. Confirmado.

---

## Pendientes pre-implementacion

- [x] ~~Crear colaborador `engram-cloud-sync@intuit` con `Role = Admin` en IntuitHub.~~ **Hecho** - id=50, asignado al team GERENCIAL (HasFullVisibility=true) para bypassar el filtro de visibilidad.
- [x] ~~Generar API key y guardarla como secreto.~~ **Hecho** - se pasa via `INTUIT_ENGRAM_INTUITHUB_ADMIN_KEY`.
- [x] ~~Confirmar URL base de IntuitHub-API por ambiente.~~ **Dev**: `https://devserver01.intuit.ar/intuit-hub-api/`. QA/prod por confirmar.
- [x] ~~Confirmar si `GET /api/collaborators` esta disponible~~ **Confirmado** - existe, requiere Admin/Editor/Viewer, retorna `Teams` embebidos con `TeamCode` y `Role` per-team.
- [ ] Decidir si Sprint 4 entra en este ciclo o se posterga.

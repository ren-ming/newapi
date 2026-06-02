# Group Admin Role: Multi-Level Permission Architecture Design

**Date**: 2026-06-02
**Status**: Approved (v4 — fail-closed guard for empty group)
**Scope**: Backend middleware + Controller scope injection + quota_data schema + Frontend API routing

---

## 1. Problem Statement

The current permission model has only 3 levels: `普通用户(1)`, `管理员(10)`, `超级管理员(100)`. We need to introduce a new `分组管理员(Group Admin)` role at level 5 that:

- Sees the same menu structure as a regular user (Dashboard, Token Management, Logs, Settings)
- Has **system-admin-level view access** on Dashboard and Logs data, but **scoped to their own group only**
- Cannot access admin-only features (Channel Management, System Settings, etc.)

## 2. Role Hierarchy

| Role | Value | Description |
|------|-------|-------------|
| Guest | 0 | Guest user |
| Common User | 1 | Regular user, sees personal data only |
| **Group Admin** | **5** | **NEW — sees group-scoped admin data** |
| Admin | 10 | Full admin, sees all data |
| Root | 100 | Super admin, system settings access |

Data boundary: A Group Admin with `user.group = 'vip'` can only see data from users where `user.group = 'vip'`.

## 3. Architecture: Middleware Layer Scope Injection

### 3.1 Refactor `authHelper` — Extract `validateAuth`

**Problem**: The existing `authHelper` internally calls `c.Next()` at the end (line 156). If we set `scope_group` *after* calling `authHelper`, the controller has already executed and cannot read `scope_group`.

**Solution**: Extract validation logic into `validateAuth()` (no `c.Next()`), then `authHelper` becomes a thin wrapper.

**File**: `middleware/auth.go`

```go
// validateAuth performs all auth validation and sets context values.
// It does NOT call c.Next() — the caller is responsible for that.
// Returns true if auth passed, false if it aborted the request.
func validateAuth(c *gin.Context, minRole int) bool {
    session := sessions.Default(c)
    username := session.Get("username")
    role := session.Get("role")
    id := session.Get("id")
    status := session.Get("status")
    group := session.Get("group")  // Pre-read from session
    useAccessToken := false

    if username == nil {
        // Check access token (same logic as current authHelper lines 44-93)
        accessToken := c.Request.Header.Get("Authorization")
        if accessToken == "" {
            c.JSON(http.StatusUnauthorized, gin.H{
                "success": false,
                "message": common.TranslateMessage(c, i18n.MsgAuthNotLoggedIn),
            })
            c.Abort()
            return false
        }
        user, authErr := model.ValidateAccessToken(accessToken)
        if authErr != nil {
            // ... error handling (same as existing) ...
            c.Abort()
            return false
        }
        if user != nil && user.Username != "" {
            if !validUserInfo(user.Username, user.Role) {
                c.JSON(http.StatusOK, gin.H{
                    "success": false,
                    "message": common.TranslateMessage(c, i18n.MsgAuthUserInfoInvalid),
                })
                c.Abort()
                return false
            }
            username = user.Username
            role = user.Role
            id = user.Id
            status = user.Status
            group = user.Group  // FIX: read group from user object, not session
            useAccessToken = true
        } else {
            c.JSON(http.StatusOK, gin.H{
                "success": false,
                "message": common.TranslateMessage(c, i18n.MsgAuthAccessTokenInvalid),
            })
            c.Abort()
            return false
        }
    }

    // User ID header check (same as existing)
    apiUserIdStr := c.Request.Header.Get("New-Api-User")
    if apiUserIdStr == "" {
        c.JSON(http.StatusUnauthorized, gin.H{
            "success": false,
            "message": common.TranslateMessage(c, i18n.MsgAuthUserIdNotProvided),
        })
        c.Abort()
        return false
    }
    apiUserId, err := strconv.Atoi(apiUserIdStr)
    if err != nil {
        c.JSON(http.StatusUnauthorized, gin.H{
            "success": false,
            "message": common.TranslateMessage(c, i18n.MsgAuthUserIdFormatError),
        })
        c.Abort()
        return false
    }
    if id != apiUserId {
        c.JSON(http.StatusUnauthorized, gin.H{
            "success": false,
            "message": common.TranslateMessage(c, i18n.MsgAuthUserIdMismatch),
        })
        c.Abort()
        return false
    }

    if status.(int) == common.UserStatusDisabled {
        c.JSON(http.StatusOK, gin.H{
            "success": false,
            "message": common.TranslateMessage(c, i18n.MsgAuthUserBanned),
        })
        c.Abort()
        return false
    }
    if role.(int) < minRole {
        c.JSON(http.StatusOK, gin.H{
            "success": false,
            "message": common.TranslateMessage(c, i18n.MsgAuthInsufficientPrivilege),
        })
        c.Abort()
        return false
    }
    if !validUserInfo(username.(string), role.(int)) {
        c.JSON(http.StatusOK, gin.H{
            "success": false,
            "message": common.TranslateMessage(c, i18n.MsgAuthUserInfoInvalid),
        })
        c.Abort()
        return false
    }

    c.Header("Auth-Version", "864b7076dbcd0a3c01b5520316720ebf")
    c.Set("username", username)
    c.Set("role", role)
    c.Set("id", id)
    c.Set("group", group)        // FIX: use unified group variable
    c.Set("user_group", group)   // FIX: use unified group variable
    c.Set("use_access_token", useAccessToken)
    return true
}

// authHelper is now a thin wrapper — existing callers unchanged.
func authHelper(c *gin.Context, minRole int) {
    if validateAuth(c, minRole) {
        c.Next()
    }
}
```

> **Bug fix note**: The original `authHelper` used `c.Set("group", session.Get("group"))` which returned `nil`
> for Access Token auth users (no session). This is a **pre-existing bug** — all Access Token users had
> empty `group` in context. The refactored `validateAuth` fixes this by reading `user.Group` from the
> `ValidateAccessToken` result, ensuring `c.Get("group")` is always populated regardless of auth method.

### 3.2 New Middleware: `AdminOrGroupAdminAuth()`

**File**: `middleware/auth.go`

```go
func AdminOrGroupAdminAuth() func(c *gin.Context) {
    return func(c *gin.Context) {
        if !validateAuth(c, common.RoleGroupAdmin) {
            return
        }
        // Inject scope_group BEFORE c.Next() — controller will see it
        role := c.GetInt("role")
        if role == common.RoleGroupAdmin {
            group := c.GetString("group")
            if group == "" {
                // Fail closed: group admin without group is a misconfiguration
                c.JSON(http.StatusForbidden, gin.H{
                    "success": false,
                    "message": "分组管理员必须属于一个分组",
                })
                c.Abort()
                return
            }
            c.Set("scope_group", group)
        }
        c.Next()
    }
}
```

> **Defense-in-depth**: A group admin with an empty `group` field is a configuration error.
> Rather than silently showing all data (fail-open), we reject the request with 403 (fail-closed).
> This prevents privilege escalation through misconfiguration.

**Key behavior**:
- `role >= 5` passes auth (group admin, admin, root)
- Only `role == 5` gets `scope_group` injected
- `role >= 10` has no scope constraint (global view)
- `scope_group` is set **before** `c.Next()`, so controllers can read it

### 3.3 Router Changes

**File**: `router/api-router.go`

Replace `AdminAuth()` with `AdminOrGroupAdminAuth()` only on routes that group admins need:

```go
// Dashboard data — open to group admin
dataRoute.GET("/", middleware.AdminOrGroupAdminAuth(), controller.GetAllQuotaDates)
dataRoute.GET("/users", middleware.AdminOrGroupAdminAuth(), controller.GetQuotaDatesByUser)

// Logs — open to group admin
logRoute.GET("/", middleware.AdminOrGroupAdminAuth(), controller.GetAllLogs)
logRoute.GET("/stat", middleware.AdminOrGroupAdminAuth(), controller.GetLogsStat)

// All other admin routes keep AdminAuth() unchanged
// e.g., channel management, system settings, etc.
```

### 3.4 Controller Changes

**Pattern** — each affected controller reads `scope_group` and forces it:

```go
// Force group scoping for group admins — overrides client-provided group param
if scopeGroup := c.GetString("scope_group"); scopeGroup != "" {
    group = scopeGroup
}
```

**Affected controllers**:

| Function | File | Change |
|----------|------|--------|
| `GetAllQuotaDates` | `controller/usedata.go` | Read `scope_group`, pass to model as `group` param |
| `GetQuotaDatesByUser` | `controller/usedata.go` | Read `scope_group`, pass to model as `group` param |
| `GetAllLogs` | `controller/log.go` | Override `group` param with `scope_group` |
| `GetLogsStat` | `controller/log.go` | Override `group` param with `scope_group` |

### 3.5 Dashboard Data Model — Add `group` Column (Schema Migration)

**Problem**: The `quota_data` table has no `group` column. It stores `{user_id, username, model_name, created_at, count, quota, token_used}` — group filtering is impossible.

**Solution (Approach A)**: Add `group` column to `quota_data` and populate it during writes.

#### 3.5.1 Schema Migration

**File**: `model/usedata.go` — update `QuotaData` struct:

```go
type QuotaData struct {
    Id          int    `json:"id"`
    UserID      int    `json:"user_id" gorm:"index"`
    Username    string `json:"username" gorm:"index:idx_qdt_model_user_name,priority:2;size:64;default:''"`
    DisplayName string `json:"display_name" gorm:"-"`
    ModelName   string `json:"model_name" gorm:"index:idx_qdt_model_user_name,priority:1;size:64;default:''"`
    CreatedAt   int64  `json:"created_at" gorm:"bigint;index:idx_qdt_created_at,priority:2"`
    TokenUsed   int    `json:"token_used" gorm:"default:0"`
    Count       int    `json:"count" gorm:"default:0"`
    Quota       int    `json:"quota" gorm:"default:0"`
    Group       string `json:"group" gorm:"type:varchar(64);index;default:''"` // NEW
}
```

**Migration** (in `model/main.go` auto-migration):

```go
// quota_data group column will be auto-created by GORM AutoMigrate
// Existing rows get default '' (empty string) — run BackfillQuotaDataGroup() once after migration
```

#### 3.5.2 Write Path — Populate `group` at Write Time

**File**: `model/usedata.go` — update `logQuotaDataCache` and `LogQuotaData`:

```go
// Add group parameter
func logQuotaDataCache(userId int, username string, modelName string, quota int, createdAt int64, tokenUsed int, group string) {
    key := fmt.Sprintf("%d-%s-%s-%d", userId, username, modelName, createdAt)
    quotaData, ok := CacheQuotaData[key]
    if ok {
        quotaData.Count += 1
        quotaData.Quota += quota
        quotaData.TokenUsed += tokenUsed
    } else {
        quotaData = &QuotaData{
            UserID:    userId,
            Username:  username,
            ModelName: modelName,
            CreatedAt: createdAt,
            Count:     1,
            Quota:     quota,
            TokenUsed: tokenUsed,
            Group:     group,  // NEW
        }
    }
    CacheQuotaData[key] = quotaData
}

func LogQuotaData(userId int, username string, modelName string, quota int, createdAt int64, tokenUsed int, group string) {
    createdAt = createdAt - (createdAt % 3600)
    CacheQuotaDataLock.Lock()
    defer CacheQuotaDataLock.Unlock()
    logQuotaDataCache(userId, username, modelName, quota, createdAt, tokenUsed, group)
}
```

**File**: `model/log.go:258` — caller update:

```go
// Before:
LogQuotaData(userId, username, params.ModelName, params.Quota, common.GetTimestamp(), params.PromptTokens+params.CompletionTokens)

// After:
LogQuotaData(userId, username, params.ModelName, params.Quota, common.GetTimestamp(), params.PromptTokens+params.CompletionTokens, params.Group)
```

#### 3.5.3 Read Path — Filter by `group`

**File**: `model/usedata.go` — add `group` parameter to query functions:

```go
func GetAllQuotaDates(startTime, endTime int64, username, group string) (quotaData []*QuotaData, err error) {
    if username != "" {
        return GetQuotaDataByUsername(username, startTime, endTime, group)
    }
    var quotaDatas []*QuotaData
    tx := DB.Table("quota_data").
        Select("model_name, sum(count) as count, sum(quota) as quota, sum(token_used) as token_used, created_at").
        Where("created_at >= ? and created_at <= ?", startTime, endTime)
    if group != "" {
        tx = tx.Where(commonGroupCol+" = ?", group)  // Cross-DB column quoting
    }
    err = tx.Group("model_name, created_at").Find(&quotaDatas).Error
    return quotaDatas, err
}

func GetQuotaDataByUsername(username string, startTime, endTime int64, group string) (quotaData []*QuotaData, err error) {
    var quotaDatas []*QuotaData
    tx := DB.Table("quota_data").
        Where("username = ? and created_at >= ? and created_at <= ?", username, startTime, endTime)
    if group != "" {
        tx = tx.Where(commonGroupCol+" = ?", group)  // FIX: also filter by group for username queries
    }
    err = tx.Find(&quotaDatas).Error
    return quotaDatas, err
}

func GetQuotaDataGroupByUser(startTime, endTime int64, group string) (quotaData []*QuotaData, err error) {
    var quotaDatas []*QuotaData
    tx := DB.Table("quota_data").
        Select("username, created_at, sum(count) as count, sum(quota) as quota, sum(token_used) as token_used").
        Where("created_at >= ? and created_at <= ?", startTime, endTime)
    if group != "" {
        tx = tx.Where(commonGroupCol+" = ?", group)
    }
    err = tx.Group("username, created_at").Find(&quotaDatas).Error
    if err != nil {
        return nil, err
    }
    fillQuotaDataDisplayNames(quotaDatas)
    return quotaDatas, nil
}
```

Log model (`model/log.go`) already supports `group` parameter via `logGroupCol` — no changes needed.

### 3.6 Controller Full Code for Dashboard

**File**: `controller/usedata.go`

```go
func GetAllQuotaDates(c *gin.Context) {
    startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
    endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
    username := c.Query("username")
    group := ""  // No group for regular admin
    // Force group scoping for group admins
    if scopeGroup := c.GetString("scope_group"); scopeGroup != "" {
        group = scopeGroup
    }
    dates, err := model.GetAllQuotaDates(startTimestamp, endTimestamp, username, group)
    if err != nil {
        common.ApiError(c, err)
        return
    }
    c.JSON(http.StatusOK, gin.H{
        "success": true,
        "message": "",
        "data":    dates,
    })
}

func GetQuotaDatesByUser(c *gin.Context) {
    startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
    endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
    group := ""
    if scopeGroup := c.GetString("scope_group"); scopeGroup != "" {
        group = scopeGroup
    }
    dates, err := model.GetQuotaDataGroupByUser(startTimestamp, endTimestamp, group)
    if err != nil {
        common.ApiError(c, err)
        return
    }
    c.JSON(http.StatusOK, gin.H{
        "success": true,
        "message": "",
        "data":    dates,
    })
}
```

## 4. Frontend Changes

### 4.1 Role Helper

**File**: `web/classic/src/helpers/utils.jsx`

```jsx
export function isGroupAdmin() {
    let user = localStorage.getItem('user');
    if (!user) return false;
    user = JSON.parse(user);
    return user.role === 5;
}
```

### 4.2 Dashboard Data Fetching

**File**: `web/classic/src/hooks/dashboard/useDashboardData.js`

Change API endpoint selection:

```jsx
// Before: isAdmin() ? '/api/data/' : '/api/data/self/'
// After:  (isAdmin() || isGroupAdmin()) ? '/api/data/' : '/api/data/self/'
```

The backend automatically injects group constraint for role=5, so no `group` parameter needed in the request.

### 4.3 Logs Data Fetching

**File**: `web/classic/src/hooks/usage-logs/useUsageLogsData.jsx`

Same pattern:

```jsx
// Before: isAdmin() ? '/api/log/' : '/api/log/self/'
// After:  (isAdmin() || isGroupAdmin()) ? '/api/log/' : '/api/log/self/'
```

### 4.4 UI Visibility

For admin-only UI elements (columns, charts), extend the condition:

```jsx
// Before: isAdmin() ? <AdminColumn /> : null
// After:  (isAdmin() || isGroupAdmin()) ? <AdminColumn /> : null
```

This applies to:
- Dashboard: user consumption charts (tabs 5-8)
- Logs: Username, Channel, Retry columns

### 4.5 Menu System

**No changes needed.** Group admin's menu structure matches regular user:
- `isAdmin()` returns false for role=5, so the "管理员" sidebar section is hidden
- Dashboard and Logs pages are under `PrivateRoute`, accessible to all authenticated users

## 5. Security Guarantees

| Constraint | Mechanism |
|------------|-----------|
| No frontend data filtering | Backend middleware forces `scope_group` into SQL WHERE clause |
| No scope bypass via API params | Controller overrides client-provided `group` with `scope_group` |
| No scope bypass via Access Token | `validateAuth` reads `user.Group` from token validation result, not session |
| No scope bypass via username query | `GetQuotaDataByUsername` also filters by `scope_group` |
| No scope bypass via empty group | `AdminOrGroupAdminAuth` rejects (403) group admins with empty `group` field |
| Admin-only features protected | `AdminAuth()` (role >= 10) unchanged on channel/settings routes |
| Regular users unaffected | All conditions use `isAdmin() \|\| isGroupAdmin()`, false for role=1 |
| No component duplication | Dashboard/Logs components have zero structural changes |
| Cross-DB column quoting | Uses `commonGroupCol` / `logGroupCol` variables, not raw `"group"` |
| Cross-DB backfill | `BackfillQuotaDataGroup()` uses GORM, no raw SQL with reserved words |

## 6. Files Changed

### Backend (Go)
1. `common/constants.go` — Add `RoleGroupAdmin = 5`, update `IsValidateRole()`
2. `middleware/auth.go` — Refactor `authHelper` → `validateAuth` + `authHelper` wrapper, add `AdminOrGroupAdminAuth()`, fix Access Token group bug
3. `router/api-router.go` — Replace middleware on 4 routes
4. `controller/usedata.go` — Read `scope_group` in 2 functions, pass to model
5. `controller/log.go` — Read `scope_group` in 2 functions, override `group` param
6. `model/usedata.go` — Add `Group` field to `QuotaData`, add `group` param to all query functions (including `GetQuotaDataByUsername`) + write path + `BackfillQuotaDataGroup()`
7. `model/log.go` — Pass `params.Group` to `LogQuotaData()` call (1 line change)

### Frontend (React)
1. `web/classic/src/helpers/utils.jsx` — Add `isGroupAdmin()`
2. `web/classic/src/hooks/dashboard/useDashboardData.js` — Extend API condition
3. `web/classic/src/hooks/usage-logs/useUsageLogsData.jsx` — Extend API condition
4. `web/classic/src/components/dashboard/ChartsPanel.jsx` — Extend admin-only chart visibility
5. `web/classic/src/components/table/usage-logs/UsageLogsColumnDefs.jsx` — Extend admin-only columns

### Database Migration
- `quota_data` table: new `group` column (varchar(64), default '', indexed) — auto-created by GORM AutoMigrate
- One-time backfill in Go (cross-DB compatible, avoids raw SQL with reserved word `group`):

```go
// BackfillQuotaDataGroup populates the group column for existing quota_data rows.
// Safe to run multiple times — only updates rows where group is empty.
func BackfillQuotaDataGroup() {
    var usernames []string
    DB.Table("quota_data").
        Where(commonGroupCol + " = ''").
        Distinct("username").
        Pluck("username", &usernames)
    if len(usernames) == 0 {
        return
    }
    for _, username := range usernames {
        var user User
        if err := DB.Where("username = ?", username).Select(commonGroupCol).First(&user).Error; err != nil {
            continue // user not found, skip
        }
        if user.Group != "" {
            DB.Table("quota_data").
                Where("username = ?", username).
                Where(commonGroupCol + " = ''").
                Update(commonGroupCol, user.Group)
        }
    }
}
```

## 7. Implementation Sequence

1. Backend constants + `IsValidateRole` update
2. `quota_data` schema migration + backfill
3. `model/usedata.go` — add `Group` field + write/read path changes
4. `model/log.go` — pass group to `LogQuotaData`
5. Middleware refactor (`validateAuth` + `AdminOrGroupAdminAuth`)
6. Router changes (4 routes)
7. Controller changes (4 functions)
8. Frontend changes (5 files)
9. Manual verification with role=5 test user

## 8. Out of Scope

- User management by group admin (future iteration — same menu as regular user, only sees own profile)
- Token management scoping (future iteration — same menu as regular user, only sees own tokens)
- Default UI (`web/default/`) changes (only classic UI for now)
- E2E tests (unit tests cover the scope injection logic)

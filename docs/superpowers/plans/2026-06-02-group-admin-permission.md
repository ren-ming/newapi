# Group Admin Permission Architecture — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Introduce a `Group Admin (role=5)` that sees group-scoped Dashboard and Logs data via middleware-injected `scope_group`, while keeping the same UI components and menu as a regular user.

**Architecture:** Refactor `authHelper` into `validateAuth` (no `c.Next()`) so `AdminOrGroupAdminAuth` can inject `scope_group` before controllers execute. Add `group` column to `quota_data` table. Controllers read `scope_group` and pass it as a hard filter to model queries. Frontend extends `isAdmin()` conditions with `isGroupAdmin()`.

**Tech Stack:** Go 1.22+ / Gin / GORM, React 18 (Classic UI), Semi Design

**Design Spec:** `docs/superpowers/specs/2026-06-02-group-admin-permission-architecture-design.md` (v4)

---

## File Structure

### Backend (Go) — Modified Files
| File | Responsibility |
|------|---------------|
| `common/constants.go:186-195` | Add `RoleGroupAdmin = 5`, update `IsValidateRole()` |
| `middleware/auth.go:36-157` | Refactor `authHelper` → `validateAuth` + wrapper, add `AdminOrGroupAdminAuth()` |
| `router/api-router.go:296-309` | Replace `AdminAuth()` with `AdminOrGroupAdminAuth()` on 4 routes |
| `model/usedata.go` (full file) | Add `Group` field to `QuotaData`, update write path + all read functions + backfill |
| `model/log.go:256-259` | Pass `params.Group` to `LogQuotaData` |
| `controller/usedata.go` (full file) | Read `scope_group` in `GetAllQuotaDates` + `GetQuotaDatesByUser` |
| `controller/log.go:13-33,98-123` | Read `scope_group` in `GetAllLogs` + `GetLogsStat` |

### Frontend (React) — Modified Files
| File | Responsibility |
|------|---------------|
| `web/classic/src/helpers/utils.jsx` | Add `isGroupAdmin()` export |
| `web/classic/src/hooks/dashboard/useDashboardData.js` | Extend `isAdmin` → `isAdmin \|\| isGroupAdmin` for API + user data |
| `web/classic/src/hooks/usage-logs/useUsageLogsData.jsx` | Extend `isAdmin` → `isAdmin \|\| isGroupAdmin` for all conditions |
| `web/classic/src/components/dashboard/ChartsPanel.jsx` | Extend chart tab visibility |
| `web/classic/src/components/table/usage-logs/UsageLogsColumnDefs.jsx` | Extend column visibility |

---

### Task 1: Add RoleGroupAdmin Constant

**Files:**
- Modify: `common/constants.go:186-195`

- [ ] **Step 1: Add constant and update validator**

In `common/constants.go`, change the role constants block and `IsValidateRole`:

```go
// Before (lines 186-195):
const (
    RoleGuestUser  = 0
    RoleCommonUser = 1
    RoleAdminUser  = 10
    RoleRootUser   = 100
)

func IsValidateRole(role int) bool {
    return role == RoleGuestUser || role == RoleCommonUser || role == RoleAdminUser || role == RoleRootUser
}

// After:
const (
    RoleGuestUser  = 0
    RoleCommonUser = 1
    RoleGroupAdmin = 5
    RoleAdminUser  = 10
    RoleRootUser   = 100
)

func IsValidateRole(role int) bool {
    return role == RoleGuestUser || role == RoleCommonUser || role == RoleGroupAdmin || role == RoleAdminUser || role == RoleRootUser
}
```

- [ ] **Step 2: Verify compilation**

Run: `go build ./...`
Expected: compiles with no errors.

- [ ] **Step 3: Commit**

```bash
git add common/constants.go
git commit -m "feat: add RoleGroupAdmin constant (role=5)"
```

---

### Task 2: Add Group Field to QuotaData + Backfill + Write Path

**Files:**
- Modify: `model/usedata.go` (full file)

- [ ] **Step 1: Add Group field to QuotaData struct**

In `model/usedata.go`, add `Group` field to the `QuotaData` struct (after `Quota` field, around line 22):

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
    Group       string `json:"group" gorm:"type:varchar(64);index;default:''"`
}
```

- [ ] **Step 2: Update logQuotaDataCache to accept and store group**

Change `logQuotaDataCache` signature to add `group string` param and populate `Group` in the new entry:

```go
// Before:
func logQuotaDataCache(userId int, username string, modelName string, quota int, createdAt int64, tokenUsed int) {
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
        }
    }
    CacheQuotaData[key] = quotaData
}

// After:
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
            Group:     group,
        }
    }
    CacheQuotaData[key] = quotaData
}
```

- [ ] **Step 3: Update LogQuotaData signature**

```go
// Before:
func LogQuotaData(userId int, username string, modelName string, quota int, createdAt int64, tokenUsed int) {
    createdAt = createdAt - (createdAt % 3600)
    CacheQuotaDataLock.Lock()
    defer CacheQuotaDataLock.Unlock()
    logQuotaDataCache(userId, username, modelName, quota, createdAt, tokenUsed)
}

// After:
func LogQuotaData(userId int, username string, modelName string, quota int, createdAt int64, tokenUsed int, group string) {
    createdAt = createdAt - (createdAt % 3600)
    CacheQuotaDataLock.Lock()
    defer CacheQuotaDataLock.Unlock()
    logQuotaDataCache(userId, username, modelName, quota, createdAt, tokenUsed, group)
}
```

- [ ] **Step 4: Update read path — GetAllQuotaDates**

```go
// Before:
func GetAllQuotaDates(startTime int64, endTime int64, username string) (quotaData []*QuotaData, err error) {
    if username != "" {
        return GetQuotaDataByUsername(username, startTime, endTime)
    }
    var quotaDatas []*QuotaData
    err = DB.Table("quota_data").Select("model_name, sum(count) as count, sum(quota) as quota, sum(token_used) as token_used, created_at").Where("created_at >= ? and created_at <= ?", startTime, endTime).Group("model_name, created_at").Find(&quotaDatas).Error
    return quotaDatas, err
}

// After:
func GetAllQuotaDates(startTime int64, endTime int64, username string, group string) (quotaData []*QuotaData, err error) {
    if username != "" {
        return GetQuotaDataByUsername(username, startTime, endTime, group)
    }
    var quotaDatas []*QuotaData
    tx := DB.Table("quota_data").Select("model_name, sum(count) as count, sum(quota) as quota, sum(token_used) as token_used, created_at").Where("created_at >= ? and created_at <= ?", startTime, endTime)
    if group != "" {
        tx = tx.Where(commonGroupCol+" = ?", group)
    }
    err = tx.Group("model_name, created_at").Find(&quotaDatas).Error
    return quotaDatas, err
}
```

- [ ] **Step 5: Update read path — GetQuotaDataByUsername**

```go
// Before:
func GetQuotaDataByUsername(username string, startTime int64, endTime int64) (quotaData []*QuotaData, err error) {
    var quotaDatas []*QuotaData
    err = DB.Table("quota_data").Where("username = ? and created_at >= ? and created_at <= ?", username, startTime, endTime).Find(&quotaDatas).Error
    return quotaDatas, err
}

// After:
func GetQuotaDataByUsername(username string, startTime int64, endTime int64, group string) (quotaData []*QuotaData, err error) {
    var quotaDatas []*QuotaData
    tx := DB.Table("quota_data").Where("username = ? and created_at >= ? and created_at <= ?", username, startTime, endTime)
    if group != "" {
        tx = tx.Where(commonGroupCol+" = ?", group)
    }
    err = tx.Find(&quotaDatas).Error
    return quotaDatas, err
}
```

- [ ] **Step 6: Update read path — GetQuotaDataGroupByUser**

```go
// Before:
func GetQuotaDataGroupByUser(startTime int64, endTime int64) (quotaData []*QuotaData, err error) {
    var quotaDatas []*QuotaData
    err = DB.Table("quota_data").
        Select("username, created_at, sum(count) as count, sum(quota) as quota, sum(token_used) as token_used").
        Where("created_at >= ? and created_at <= ?", startTime, endTime).
        Group("username, created_at").
        Find(&quotaDatas).Error
    if err != nil {
        return nil, err
    }
    fillQuotaDataDisplayNames(quotaDatas)
    return quotaDatas, nil
}

// After:
func GetQuotaDataGroupByUser(startTime int64, endTime int64, group string) (quotaData []*QuotaData, err error) {
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

- [ ] **Step 7: Add BackfillQuotaDataGroup function**

Append at end of `model/usedata.go`:

```go
// BackfillQuotaDataGroup populates the group column for existing quota_data rows.
// Safe to run multiple times — only updates rows where group is empty.
func BackfillQuotaDataGroup() {
    var usernames []string
    DB.Table("quota_data").
        Where(commonGroupCol+" = ''").
        Distinct("username").
        Pluck("username", &usernames)
    if len(usernames) == 0 {
        return
    }
    for _, username := range usernames {
        var user User
        if err := DB.Where("username = ?", username).First(&user).Error; err != nil {
            continue
        }
        if user.Group != "" {
            DB.Table("quota_data").
                Where("username = ?", username).
                Where(commonGroupCol+" = ''").
                Update(commonGroupCol, user.Group)
        }
    }
}
```

- [ ] **Step 8: Verify compilation**

Run: `go build ./...`
Expected: This will FAIL because `LogQuotaData` callers still use old signature. That's expected — Task 3 fixes the caller. Skip compilation for now.

- [ ] **Step 9: Commit (staged after Task 3 compiles)**

---

### Task 3: Update LogQuotaData Caller in log.go

**Files:**
- Modify: `model/log.go:258`

- [ ] **Step 1: Pass params.Group to LogQuotaData**

In `model/log.go`, find the line (around 258) inside the `gopool.Go` callback:

```go
// Before:
gopool.Go(func() {
    LogQuotaData(userId, username, params.ModelName, params.Quota, common.GetTimestamp(), params.PromptTokens+params.CompletionTokens)
})

// After:
gopool.Go(func() {
    LogQuotaData(userId, username, params.ModelName, params.Quota, common.GetTimestamp(), params.PromptTokens+params.CompletionTokens, params.Group)
})
```

- [ ] **Step 2: Verify compilation**

Run: `go build ./...`
Expected: compiles with no errors. The `params.Group` field exists on the params struct (it's the same `Group` used to create the Log record on line 241).

- [ ] **Step 3: Commit Tasks 2+3 together**

```bash
git add model/usedata.go model/log.go
git commit -m "feat: add group column to quota_data schema + write/read path + backfill"
```

---

### Task 4: Refactor authHelper → validateAuth + Add AdminOrGroupAdminAuth

**Files:**
- Modify: `middleware/auth.go:36-157`

This is the most critical change. The entire `authHelper` function body (lines 36-157) is extracted into `validateAuth` (returns `bool`, no `c.Next()`), then `authHelper` becomes a 3-line wrapper.

- [ ] **Step 1: Replace the authHelper function block (lines 36-157) with the following**

Replace from `func authHelper(c *gin.Context, minRole int) {` (line 36) through the closing `}` (line 157) with:

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
	group := session.Get("group")
	useAccessToken := false
	if username == nil {
		// Check access token
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
			if errors.Is(authErr, model.ErrDatabase) {
				common.SysLog("ValidateAccessToken database error: " + authErr.Error())
				c.JSON(http.StatusInternalServerError, gin.H{
					"success": false,
					"message": common.TranslateMessage(c, i18n.MsgDatabaseError),
				})
			} else {
				c.JSON(http.StatusOK, gin.H{
					"success": false,
					"message": common.TranslateMessage(c, i18n.MsgAuthAccessTokenInvalid),
				})
			}
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
			group = user.Group
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
	c.Set("group", group)
	c.Set("user_group", group)
	c.Set("use_access_token", useAccessToken)
	return true
}

// authHelper is a thin wrapper around validateAuth.
// Existing callers (UserAuth, AdminAuth, RootAuth) are unchanged.
func authHelper(c *gin.Context, minRole int) {
	if validateAuth(c, minRole) {
		c.Next()
	}
}

// AdminOrGroupAdminAuth allows group admins and above to access admin-scoped routes.
// For group admins (role=5), it injects scope_group to enforce data isolation.
func AdminOrGroupAdminAuth() func(c *gin.Context) {
	return func(c *gin.Context) {
		if !validateAuth(c, common.RoleGroupAdmin) {
			return
		}
		role := c.GetInt("role")
		if role == common.RoleGroupAdmin {
			group := c.GetString("group")
			if group == "" {
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

- [ ] **Step 2: Verify compilation**

Run: `go build ./...`
Expected: compiles with no errors. `UserAuth()`, `AdminAuth()`, `RootAuth()` call `authHelper` which now delegates to `validateAuth`.

- [ ] **Step 3: Commit**

```bash
git add middleware/auth.go
git commit -m "refactor: extract validateAuth from authHelper, add AdminOrGroupAdminAuth middleware"
```

---

### Task 5: Update Router — 4 Routes

**Files:**
- Modify: `router/api-router.go:297,299,307,308`

- [ ] **Step 1: Replace AdminAuth with AdminOrGroupAdminAuth on data and log routes**

In `router/api-router.go`, change these 4 lines:

```go
// Before (lines 297, 299):
logRoute.GET("/", middleware.AdminAuth(), controller.GetAllLogs)
logRoute.GET("/stat", middleware.AdminAuth(), controller.GetLogsStat)

// After:
logRoute.GET("/", middleware.AdminOrGroupAdminAuth(), controller.GetAllLogs)
logRoute.GET("/stat", middleware.AdminOrGroupAdminAuth(), controller.GetLogsStat)

// Before (lines 307, 308):
dataRoute.GET("/", middleware.AdminAuth(), controller.GetAllQuotaDates)
dataRoute.GET("/users", middleware.AdminAuth(), controller.GetQuotaDatesByUser)

// After:
dataRoute.GET("/", middleware.AdminOrGroupAdminAuth(), controller.GetAllQuotaDates)
dataRoute.GET("/users", middleware.AdminOrGroupAdminAuth(), controller.GetQuotaDatesByUser)
```

- [ ] **Step 2: Verify compilation**

Run: `go build ./...`
Expected: compiles with no errors.

- [ ] **Step 3: Commit**

```bash
git add router/api-router.go
git commit -m "feat: open dashboard and log routes to group admin via AdminOrGroupAdminAuth"
```

---

### Task 6: Update Dashboard Controllers

**Files:**
- Modify: `controller/usedata.go` (full file)

- [ ] **Step 1: Replace entire file content**

```go
package controller

import (
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

func GetAllQuotaDates(c *gin.Context) {
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	username := c.Query("username")
	group := ""
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

func GetUserQuotaDates(c *gin.Context) {
	userId := c.GetInt("id")
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	if endTimestamp-startTimestamp > 2592000 {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "时间跨度不能超过 1 个月",
		})
		return
	}
	dates, err := model.GetQuotaDataByUserId(userId, startTimestamp, endTimestamp)
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

- [ ] **Step 2: Verify compilation**

Run: `go build ./...`
Expected: compiles with no errors.

- [ ] **Step 3: Commit**

```bash
git add controller/usedata.go
git commit -m "feat: dashboard controllers read scope_group for group admin data isolation"
```

---

### Task 7: Update Log Controllers

**Files:**
- Modify: `controller/log.go:13-33` (GetAllLogs)
- Modify: `controller/log.go:98-123` (GetLogsStat)

- [ ] **Step 1: Add scope_group override in GetAllLogs**

In `controller/log.go`, after the line `group := c.Query("group")` (line 22), add scope_group override:

```go
// Before (line 22):
group := c.Query("group")

// After:
group := c.Query("group")
if scopeGroup := c.GetString("scope_group"); scopeGroup != "" {
    group = scopeGroup
}
```

- [ ] **Step 2: Add scope_group override in GetLogsStat**

In `controller/log.go`, after the line `group := c.Query("group")` (line 106), add scope_group override:

```go
// Before (line 106):
group := c.Query("group")

// After:
group := c.Query("group")
if scopeGroup := c.GetString("scope_group"); scopeGroup != "" {
    group = scopeGroup
}
```

- [ ] **Step 3: Verify compilation**

Run: `go build ./...`
Expected: compiles with no errors.

- [ ] **Step 4: Commit**

```bash
git add controller/log.go
git commit -m "feat: log controllers read scope_group for group admin data isolation"
```

---

### Task 8: Backend Build Verification

- [ ] **Step 1: Full backend build**

Run: `go build -o new-api main.go`
Expected: binary builds successfully.

- [ ] **Step 2: Run existing tests**

Run: `go test ./...`
Expected: all existing tests pass. No tests should break since we only added parameters with backward-compatible defaults.

- [ ] **Step 3: Tag backend completion**

```bash
git tag group-admin-backend-done
```

---

### Task 9: Add isGroupAdmin() Frontend Helper

**Files:**
- Modify: `web/classic/src/helpers/utils.jsx`

- [ ] **Step 1: Add isGroupAdmin function**

Append to `web/classic/src/helpers/utils.jsx` (after the existing `isRoot` function):

```jsx
export function isGroupAdmin() {
    let user = localStorage.getItem('user');
    if (!user) return false;
    user = JSON.parse(user);
    return user.role === 5;
}
```

- [ ] **Step 2: Verify no syntax errors**

Run: `cd web/classic && bun run build`
Expected: builds with no errors.

- [ ] **Step 3: Commit**

```bash
git add web/classic/src/helpers/utils.jsx
git commit -m "feat: add isGroupAdmin() helper to classic frontend"
```

---

### Task 10: Update Dashboard Data Fetching Hook

**Files:**
- Modify: `web/classic/src/hooks/dashboard/useDashboardData.js`

- [ ] **Step 1: Import isGroupAdmin**

In the import section (line 23), add `isGroupAdmin`:

```jsx
// Before:
import { API, isAdmin, showError, timestamp2string } from '../../helpers';

// After:
import { API, isAdmin, isGroupAdmin, showError, timestamp2string } from '../../helpers';
```

- [ ] **Step 2: Extend isAdminUser to include isGroupAdmin**

At line 86:

```jsx
// Before:
const isAdminUser = isAdmin();

// After:
const isAdminUser = isAdmin() || isGroupAdmin();
```

This single variable change propagates to all downstream usages:
- Line 167-170: API endpoint selection (`/api/data/` vs `/api/data/self/`)
- Line 217-235: `loadUserQuotaData` guard + API call to `/api/data/users`
- Chart data processing

- [ ] **Step 3: Verify build**

Run: `cd web/classic && bun run build`
Expected: builds with no errors.

- [ ] **Step 4: Commit**

```bash
git add web/classic/src/hooks/dashboard/useDashboardData.js
git commit -m "feat: dashboard data hook extends admin API to group admin"
```

---

### Task 11: Update Usage Logs Data Hook

**Files:**
- Modify: `web/classic/src/hooks/usage-logs/useUsageLogsData.jsx`

- [ ] **Step 1: Import isGroupAdmin**

In the import section (around line 26), add `isGroupAdmin`:

```jsx
// Before:
isAdmin,

// After:
isAdmin,
isGroupAdmin,
```

- [ ] **Step 2: Extend isAdminUser**

At line 80:

```jsx
// Before:
const isAdminUser = isAdmin();

// After:
const isAdminUser = isAdmin() || isGroupAdmin();
```

This propagates to all 18+ downstream usages (column visibility, API endpoint selection, data formatting, admin-only detail display).

- [ ] **Step 3: Verify build**

Run: `cd web/classic && bun run build`
Expected: builds with no errors.

- [ ] **Step 4: Commit**

```bash
git add web/classic/src/hooks/usage-logs/useUsageLogsData.jsx
git commit -m "feat: usage logs hook extends admin API and columns to group admin"
```

---

### Task 12: Update ChartsPanel Visibility

**Files:**
- Modify: `web/classic/src/components/dashboard/ChartsPanel.jsx`

- [ ] **Step 1: Import isGroupAdmin**

In the import section (around line 40), add `isGroupAdmin` to the destructured props or import:

Check where `isAdminUser` comes from. It's a prop passed from the parent. The parent (`index.jsx` or the dashboard hook) controls this value. Since Task 10 already changed `isAdminUser = isAdmin() || isGroupAdmin()`, the prop value is already correct.

**Verify**: Read `ChartsPanel.jsx` to confirm `isAdminUser` is a prop, not a direct `isAdmin()` call.

If `isAdminUser` is a prop → **no changes needed** in ChartsPanel. The parent already passes the correct value.

If `isAdminUser` is computed inside ChartsPanel → change to `isAdmin() || isGroupAdmin()`.

- [ ] **Step 2: Verify build**

Run: `cd web/classic && bun run build`
Expected: builds with no errors.

- [ ] **Step 3: Commit only if changes were needed**

---

### Task 13: Update UsageLogsColumnDefs Visibility

**Files:**
- Modify: `web/classic/src/components/table/usage-logs/UsageLogsColumnDefs.jsx`

- [ ] **Step 1: Check how column visibility is controlled**

The column visibility is controlled by `useUsageLogsData.jsx` (Task 11 already updated `isAdminUser`). If `UsageLogsColumnDefs.jsx` receives visibility as a prop → **no changes needed**.

If `UsageLogsColumnDefs.jsx` calls `isAdmin()` directly → add `isGroupAdmin()` to the condition.

- [ ] **Step 2: Verify build**

Run: `cd web/classic && bun run build`
Expected: builds with no errors.

- [ ] **Step 3: Commit only if changes were needed**

---

### Task 14: Full Stack Build + Manual Verification

- [ ] **Step 1: Full backend build**

Run: `go build -o new-api main.go`

- [ ] **Step 2: Full frontend build**

Run: `cd web/classic && bun run build`

- [ ] **Step 3: Run backend tests**

Run: `go test ./...`

- [ ] **Step 4: Manual verification checklist**

Start the application and verify:

1. **Admin (role=10)**: Dashboard shows global data, Logs show all logs — unchanged behavior
2. **Regular user (role=1)**: Dashboard shows personal data, Logs show personal logs — unchanged behavior
3. **Group admin (role=5)**:
   - Menu shows same items as regular user (no admin section)
   - Dashboard calls `/api/data/` (not `/api/data/self/`)
   - Dashboard data is filtered to the group admin's group
   - Dashboard charts tabs 5-8 (user rankings) are visible
   - Logs calls `/api/log/` (not `/api/log/self/`)
   - Logs data is filtered to the group admin's group
   - Logs columns: Username, Channel, Retry are visible
4. **Group admin with empty group (role=5, group="")**:
   - API returns 403 "分组管理员必须属于一个分组"
5. **Group admin via Access Token**:
   - `scope_group` is correctly set from `user.Group`

- [ ] **Step 5: Final commit**

```bash
git add -A
git commit -m "feat: complete group admin permission architecture (role=5)"
```

---

## Self-Review Checklist

### 1. Spec Coverage

| Spec Section | Task |
|---|---|
| RoleGroupAdmin = 5, IsValidateRole | Task 1 |
| quota_data Group field + schema | Task 2 (Step 1) |
| Write path (LogQuotaData + logQuotaDataCache) | Task 2 (Steps 2-3) |
| Read path (GetAllQuotaDates, GetQuotaDataByUsername, GetQuotaDataGroupByUser) | Task 2 (Steps 4-6) |
| BackfillQuotaDataGroup | Task 2 (Step 7) |
| LogQuotaData caller in log.go | Task 3 |
| validateAuth refactor (c.Next fix + Access Token group fix) | Task 4 |
| AdminOrGroupAdminAuth (scope_group + empty group guard) | Task 4 |
| Router 4 routes | Task 5 |
| Dashboard controllers (scope_group → model) | Task 6 |
| Log controllers (scope_group override) | Task 7 |
| Frontend isGroupAdmin() | Task 9 |
| Dashboard hook API condition | Task 10 |
| Logs hook API + column visibility | Task 11 |
| ChartsPanel visibility | Task 12 |
| UsageLogsColumnDefs visibility | Task 13 |
| Manual verification | Task 14 |

### 2. Placeholder Scan

No TBD, TODO, or placeholder patterns found. All steps contain exact code or explicit verification commands.

### 3. Type Consistency

- `LogQuotaData(..., group string)` signature matches caller `params.Group` (string field on params struct)
- `GetAllQuotaDates(startTime, endTime, username, group string)` matches controller call `model.GetAllQuotaDates(startTimestamp, endTimestamp, username, group)`
- `GetQuotaDataByUsername(username, startTime, endTime, group string)` matches caller in `GetAllQuotaDates`
- `GetQuotaDataGroupByUser(startTime, endTime, group string)` matches controller call
- `validateAuth` returns `bool`, `authHelper` consumes it — consistent
- Frontend `isGroupAdmin()` returns `boolean`, used in `||` with `isAdmin()` — consistent

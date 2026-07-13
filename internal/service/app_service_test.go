package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/JiXiangTools/ms-sar-dashboard/internal/apperror"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/audit"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/domain"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/repository"
)

func TestAppServiceListAuthorizedAppsReturnsCachedIndex(t *testing.T) {
	repo := &fakeAppRepository{
		apps: map[int64]domain.App{
			1: testApp(1, "alpha", "secret-1"),
		},
	}
	cache := &fakeAppRedis{
		stringValues: map[string]string{
			appAuthAllAppIDsKey(): `[{"appid":1,"disabled":false},{"appid":2,"disabled":false}]`,
		},
	}
	service := newTestAppService(repo, cache)

	summaries, err := service.ListAuthorizedApps(context.Background())
	if err != nil {
		t.Fatalf("list authorized apps: %v", err)
	}

	want := []AppAuthSummary{
		{AppID: 1, Disabled: false},
		{AppID: 2, Disabled: false},
	}
	if !reflect.DeepEqual(summaries, want) {
		t.Fatalf("unexpected summaries: %#v", summaries)
	}
	if repo.listEnabledAppsCalls != 0 {
		t.Fatalf("expected cache hit to skip repository, got %d calls", repo.listEnabledAppsCalls)
	}
}

func TestAppServiceListAuthorizedAppsLoadsRepositoryAndCachesOnMiss(t *testing.T) {
	repo := &fakeAppRepository{
		apps: map[int64]domain.App{
			1: testApp(1, "alpha", "secret-1"),
			2: testApp(2, "beta", "secret-2"),
			3: func() domain.App {
				app := testApp(3, "gamma", "secret-3")
				app.Disabled = true
				return app
			}(),
		},
	}
	cache := &fakeAppRedis{}
	service := newTestAppService(repo, cache)

	summaries, err := service.ListAuthorizedApps(context.Background())
	if err != nil {
		t.Fatalf("list authorized apps: %v", err)
	}

	want := []AppAuthSummary{
		{AppID: 1, Disabled: false},
		{AppID: 2, Disabled: false},
	}
	if !reflect.DeepEqual(summaries, want) {
		t.Fatalf("unexpected summaries: %#v", summaries)
	}
	if repo.listEnabledAppsCalls != 1 {
		t.Fatalf("expected one repository call, got %d", repo.listEnabledAppsCalls)
	}
	assertCachedAppAuthSummaries(t, cache.stringValues[appAuthAllAppIDsKey()], want)
}

func TestAppServiceRebuildsAuthorizationProjectionAfterRedisFlush(t *testing.T) {
	repo := &fakeAppRepository{apps: map[int64]domain.App{
		1: testApp(1, "alpha", "secret-1"),
		2: testApp(2, "beta", "secret-2"),
	}}
	cache := &fakeAppRedis{}
	service := newTestAppService(repo, cache)

	if err := service.RebuildAuthorizationProjection(context.Background()); err != nil {
		t.Fatalf("initial rebuild: %v", err)
	}
	cache.hashes = make(map[string]map[string]string)
	cache.stringValues = make(map[string]string)

	app, err := service.Authorize(context.Background(), "2", "secret-2")
	if err != nil {
		t.Fatalf("authorize after redis flush: %v", err)
	}
	if app.ID != 2 {
		t.Fatalf("expected app 2, got %d", app.ID)
	}
	if got := cache.hashValue(appAuthKey(1), "secret"); got != "secret-1" {
		t.Fatalf("expected full projection rebuild, got app 1 secret %q", got)
	}
	assertCachedAppAuthSummaries(t, cache.stringValues[appAuthAllAppIDsKey()], []AppAuthSummary{
		{AppID: 1, Disabled: false},
		{AppID: 2, Disabled: false},
	})
}

func TestAppServiceUsesRedisKeyPrefix(t *testing.T) {
	repo := &fakeAppRepository{apps: map[int64]domain.App{1: testApp(1, "alpha", "secret-1")}}
	cache := &fakeAppRedis{}
	service := newTestAppService(repo, cache)
	service.keyPrefix = "dashboard"

	if err := service.RebuildAuthorizationProjection(context.Background()); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if got := cache.hashValue("dashboard:app_auth_1", "secret"); got != "secret-1" {
		t.Fatalf("expected prefixed app key, got secret %q", got)
	}
	if _, ok := cache.stringValues["dashboard:app_auth_allappids"]; !ok {
		t.Fatal("expected prefixed app index key")
	}
}

func TestAppServiceRebuildRetriesTransientRedisFailure(t *testing.T) {
	repo := &fakeAppRepository{apps: map[int64]domain.App{1: testApp(1, "alpha", "secret-1")}}
	cache := &fakeAppRedis{
		failAfterWriteRemaining: map[string]int{appAuthKey(1): 1},
		failErr:                 errors.New("temporary redis failure"),
	}
	service := newTestAppService(repo, cache)

	if err := service.RebuildAuthorizationProjection(context.Background()); err != nil {
		t.Fatalf("expected transient failure to be retried: %v", err)
	}
	if got := cache.hashValue(appAuthKey(1), "secret"); got != "secret-1" {
		t.Fatalf("expected rebuilt authorization, got %q", got)
	}
	if repo.listEnabledAppsCalls != 2 {
		t.Fatalf("expected two rebuild attempts, got %d", repo.listEnabledAppsCalls)
	}
}

func TestAppServiceRedisReadFailureIsServiceUnavailable(t *testing.T) {
	service := newTestAppService(&fakeAppRepository{}, &fakeAppRedis{hgetErr: errors.New("redis unavailable")})

	_, err := service.Authorize(context.Background(), "1", "secret-1")
	appErr, ok := apperror.As(err)
	if !ok || appErr.HTTPStatus != http.StatusServiceUnavailable {
		t.Fatalf("expected service unavailable, got %v", err)
	}
	if got := AppAuthFailureReason(err); got != appAuthReasonRedisReadFailed {
		t.Fatalf("expected redis failure reason, got %q", got)
	}
}

func TestAppServiceCompensationUsesIndependentContext(t *testing.T) {
	repo := &fakeAppRepository{nextID: 1}
	cache := &fakeAppRedis{
		failAfterWriteRemaining: map[string]int{appAuthKey(1): 1},
		failErr:                 errors.New("redis write failed"),
	}
	service := newTestAppService(repo, cache)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := service.Create(ctx, 7, AppCreateInput{Name: "alpha", Secret: "secret-1"}); err == nil {
		t.Fatal("expected create to fail")
	}
	if repo.hardDeleteContextErr != nil {
		t.Fatalf("expected independent compensation context, got %v", repo.hardDeleteContextErr)
	}
}

func TestAppServiceCreateRefreshesAllAppAuthKeys(t *testing.T) {
	repo := &fakeAppRepository{
		nextID: 2,
		apps: map[int64]domain.App{
			1: testApp(1, "alpha", "secret-1"),
		},
	}
	cache := &fakeAppRedis{}
	service := newTestAppService(repo, cache)

	created, err := service.Create(context.Background(), 7, AppCreateInput{
		Name:   "beta",
		Secret: "secret-2",
		Remark: "new app",
	})
	if err != nil {
		t.Fatalf("create app: %v", err)
	}
	if created.ID != 2 {
		t.Fatalf("expected new app id 2, got %d", created.ID)
	}
	if !reflect.DeepEqual(cache.hsetKeys, []string{appAuthKey(1), appAuthKey(2)}) {
		t.Fatalf("expected all active app auth keys to refresh, got %#v", cache.hsetKeys)
	}
	if got := cache.hashValue(appAuthKey(1), "secret"); got != "secret-1" {
		t.Fatalf("expected existing app secret to refresh, got %q", got)
	}
	if got := cache.hashValue(appAuthKey(2), "secret"); got != "secret-2" {
		t.Fatalf("expected new app secret to refresh, got %q", got)
	}
	assertCachedAppAuthSummaries(t, cache.stringValues[appAuthAllAppIDsKey()], []AppAuthSummary{
		{AppID: 1, Disabled: false},
		{AppID: 2, Disabled: false},
	})
}

func TestAppServiceUpdateRefreshesAllAppAuthKeys(t *testing.T) {
	repo := &fakeAppRepository{
		nextID: 3,
		apps: map[int64]domain.App{
			1: testApp(1, "alpha", "secret-1"),
			2: testApp(2, "beta", "secret-2"),
		},
	}
	cache := &fakeAppRedis{}
	service := newTestAppService(repo, cache)

	updated, err := service.Update(context.Background(), 7, 2, AppUpdateInput{
		Secret: stringPtr("secret-2-new"),
	})
	if err != nil {
		t.Fatalf("update app: %v", err)
	}
	if updated.Secret != "secret-2-new" {
		t.Fatalf("expected updated secret, got %q", updated.Secret)
	}
	if !reflect.DeepEqual(cache.hsetKeys, []string{appAuthKey(1), appAuthKey(2)}) {
		t.Fatalf("expected all active app auth keys to refresh, got %#v", cache.hsetKeys)
	}
	if got := cache.hashValue(appAuthKey(1), "secret"); got != "secret-1" {
		t.Fatalf("expected unrelated app secret to refresh, got %q", got)
	}
	if got := cache.hashValue(appAuthKey(2), "secret"); got != "secret-2-new" {
		t.Fatalf("expected updated app secret to refresh, got %q", got)
	}
	assertCachedAppAuthSummaries(t, cache.stringValues[appAuthAllAppIDsKey()], []AppAuthSummary{
		{AppID: 1, Disabled: false},
		{AppID: 2, Disabled: false},
	})
}

func TestAppServiceDeleteRefreshesAuthorizedAppIndex(t *testing.T) {
	repo := &fakeAppRepository{
		nextID: 3,
		apps: map[int64]domain.App{
			1: testApp(1, "alpha", "secret-1"),
			2: testApp(2, "beta", "secret-2"),
		},
	}
	cache := &fakeAppRedis{
		hashes: map[string]map[string]string{
			appAuthKey(1): {"secret": "secret-1"},
			appAuthKey(2): {"secret": "secret-2"},
		},
		stringValues: map[string]string{
			appAuthAllAppIDsKey(): `[{"appid":1,"disabled":false},{"appid":2,"disabled":false}]`,
		},
	}
	service := newTestAppService(repo, cache)

	if err := service.Delete(context.Background(), 7, 2); err != nil {
		t.Fatalf("delete app: %v", err)
	}
	if _, ok := cache.hashes[appAuthKey(2)]; ok {
		t.Fatal("expected deleted app auth key to be removed")
	}
	assertCachedAppAuthSummaries(t, cache.stringValues[appAuthAllAppIDsKey()], []AppAuthSummary{
		{AppID: 1, Disabled: false},
	})
}

func TestAppServiceCreateRollbackRemovesNewRedisKeyWhenRefreshFails(t *testing.T) {
	repo := &fakeAppRepository{
		nextID: 2,
		apps: map[int64]domain.App{
			1: testApp(1, "alpha", "secret-1"),
		},
	}
	cache := &fakeAppRedis{
		failAfterWriteRemaining: map[string]int{
			appAuthKey(2): 1,
		},
		failErr: errors.New("redis write failed"),
	}
	service := newTestAppService(repo, cache)

	if _, err := service.Create(context.Background(), 7, AppCreateInput{
		Name:   "beta",
		Secret: "secret-2",
	}); err == nil {
		t.Fatal("expected create to fail when auth refresh fails")
	}
	if _, ok := repo.apps[2]; ok {
		t.Fatal("expected failed create to hard delete the new app")
	}
	if _, ok := cache.hashes[appAuthKey(2)]; ok {
		t.Fatal("expected failed create to remove the new redis auth key")
	}
	if got := cache.hashValue(appAuthKey(1), "secret"); got != "secret-1" {
		t.Fatalf("expected existing app auth data to remain valid, got %q", got)
	}
	assertCachedAppAuthSummaries(t, cache.stringValues[appAuthAllAppIDsKey()], []AppAuthSummary{
		{AppID: 1, Disabled: false},
	})
}

func TestAppServiceUpdateRollbackRestoresRedisKeyWhenRefreshFails(t *testing.T) {
	repo := &fakeAppRepository{
		nextID: 3,
		apps: map[int64]domain.App{
			1: testApp(1, "alpha", "secret-1"),
			2: testApp(2, "beta", "secret-2-old"),
		},
	}
	cache := &fakeAppRedis{
		failAfterWriteRemaining: map[string]int{
			appAuthKey(2): 1,
		},
		failErr: errors.New("redis write failed"),
	}
	service := newTestAppService(repo, cache)

	if _, err := service.Update(context.Background(), 7, 2, AppUpdateInput{
		Secret: stringPtr("secret-2-new"),
	}); err == nil {
		t.Fatal("expected update to fail when auth refresh fails")
	}
	if got := repo.apps[2].Secret; got != "secret-2-old" {
		t.Fatalf("expected repository rollback to restore old secret, got %q", got)
	}
	if got := cache.hashValue(appAuthKey(2), "secret"); got != "secret-2-old" {
		t.Fatalf("expected redis rollback to restore old secret, got %q", got)
	}
	if !reflect.DeepEqual(cache.hsetKeys, []string{appAuthKey(1), appAuthKey(2), appAuthKey(1), appAuthKey(2)}) {
		t.Fatalf("expected rollback to rewrite the failed app auth key, got %#v", cache.hsetKeys)
	}
	assertCachedAppAuthSummaries(t, cache.stringValues[appAuthAllAppIDsKey()], []AppAuthSummary{
		{AppID: 1, Disabled: false},
		{AppID: 2, Disabled: false},
	})
}

func TestAppServiceDeleteRollbackRestoresRedisKeyAndIndexWhenIndexRefreshFails(t *testing.T) {
	repo := &fakeAppRepository{
		nextID: 3,
		apps: map[int64]domain.App{
			1: testApp(1, "alpha", "secret-1"),
			2: testApp(2, "beta", "secret-2"),
		},
	}
	cache := &fakeAppRedis{
		hashes: map[string]map[string]string{
			appAuthKey(1): {"secret": "secret-1"},
			appAuthKey(2): {"secret": "secret-2"},
		},
		stringValues: map[string]string{
			appAuthAllAppIDsKey(): `[{"appid":1,"disabled":false},{"appid":2,"disabled":false}]`,
		},
		failAfterSetRemaining: map[string]int{
			appAuthAllAppIDsKey(): 1,
		},
		failErr: errors.New("redis set failed"),
	}
	service := newTestAppService(repo, cache)

	if err := service.Delete(context.Background(), 7, 2); err == nil {
		t.Fatal("expected delete to fail when app index refresh fails")
	}
	if got := repo.apps[2].Disabled; got {
		t.Fatal("expected repository rollback to restore deleted app")
	}
	if got := cache.hashValue(appAuthKey(2), "secret"); got != "secret-2" {
		t.Fatalf("expected deleted app auth key to be restored, got %q", got)
	}
	assertCachedAppAuthSummaries(t, cache.stringValues[appAuthAllAppIDsKey()], []AppAuthSummary{
		{AppID: 1, Disabled: false},
		{AppID: 2, Disabled: false},
	})
}

func TestAppServiceUpdateCompensationDoesNotReenableConcurrentlyDeletedApp(t *testing.T) {
	repo := &fakeAppRepository{
		apps: map[int64]domain.App{
			1: testApp(1, "alpha", "secret-1"),
			2: testApp(2, "beta", "secret-2"),
		},
	}
	repo.onListEnabledApps = func() {
		app := repo.apps[2]
		app.Disabled = true
		app.LastUpdateTime = app.LastUpdateTime.Add(time.Second)
		repo.apps[2] = app
	}
	cache := &fakeAppRedis{
		hashes: map[string]map[string]string{
			appAuthKey(1): {"secret": "secret-1"},
			appAuthKey(2): {"secret": "secret-2"},
		},
		stringValues: map[string]string{
			appAuthAllAppIDsKey(): `[{"appid":1,"disabled":false},{"appid":2,"disabled":false}]`,
		},
		failAfterSetRemaining: map[string]int{appAuthAllAppIDsKey(): 1},
		failErr:               errors.New("redis set failed"),
	}
	service := newTestAppService(repo, cache)

	if _, err := service.Update(context.Background(), 7, 2, AppUpdateInput{Name: stringPtr("beta-updated")}); err == nil {
		t.Fatal("expected update to report projection failure")
	}
	if !repo.apps[2].Disabled {
		t.Fatal("expected concurrent delete to remain disabled")
	}
	if _, ok := cache.hashes[appAuthKey(2)]; ok {
		t.Fatal("expected compensation reconciliation to remove deleted app authorization")
	}
}

func TestAppServiceRejectsConcurrentUpdate(t *testing.T) {
	repo := &fakeAppRepository{apps: map[int64]domain.App{
		2: testApp(2, "beta", "secret-2"),
	}}
	repo.onUpdateApp = func() {
		concurrent := repo.apps[2]
		concurrent.Name = "changed-elsewhere"
		concurrent.LastUpdateTime = concurrent.LastUpdateTime.Add(time.Second)
		repo.apps[2] = concurrent
	}
	service := newTestAppService(repo, &fakeAppRedis{})

	_, err := service.Update(context.Background(), 7, 2, AppUpdateInput{Name: stringPtr("my-change")})
	appErr, ok := apperror.As(err)
	if !ok || appErr.HTTPStatus != http.StatusConflict {
		t.Fatalf("expected conflict, got %v", err)
	}
	if got := repo.apps[2].Name; got != "changed-elsewhere" {
		t.Fatalf("concurrent update was overwritten: %q", got)
	}
}

func TestAppServiceCreateLogsRefreshedAppIDsAndSuccess(t *testing.T) {
	repo := &fakeAppRepository{
		nextID: 2,
		apps: map[int64]domain.App{
			1: testApp(1, "alpha", "secret-1"),
		},
	}
	cache := &fakeAppRedis{}
	var logs bytes.Buffer
	service := newTestAppServiceWithLogger(repo, cache, log.New(&logs, "", 0))

	if _, err := service.Create(context.Background(), 7, AppCreateInput{
		Name:   "beta",
		Secret: "secret-2",
	}); err != nil {
		t.Fatalf("create app: %v", err)
	}

	output := logs.String()
	for _, want := range []string{
		"event=app.auth_refresh",
		"refresh_app_ids=1,2",
		"refreshed_app_ids=1,2",
		"refresh_success=true",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected refresh log to contain %q, got %s", want, output)
		}
	}
}

func TestAppServiceUpdateLogsFailedRefreshAppIDAndFailure(t *testing.T) {
	repo := &fakeAppRepository{
		nextID: 3,
		apps: map[int64]domain.App{
			1: testApp(1, "alpha", "secret-1"),
			2: testApp(2, "beta", "secret-2-old"),
		},
	}
	cache := &fakeAppRedis{
		failAfterWriteRemaining: map[string]int{
			appAuthKey(2): 1,
		},
		failErr: errors.New("redis write failed"),
	}
	var logs bytes.Buffer
	service := newTestAppServiceWithLogger(repo, cache, log.New(&logs, "", 0))

	if _, err := service.Update(context.Background(), 7, 2, AppUpdateInput{
		Secret: stringPtr("secret-2-new"),
	}); err == nil {
		t.Fatal("expected update to fail when auth refresh fails")
	}

	output := logs.String()
	for _, want := range []string{
		"event=app.auth_refresh",
		"refresh_app_ids=1,2",
		"refreshed_app_ids=1",
		"failed_refresh_app_id=2",
		"refresh_success=false",
		`error="redis write failed"`,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected refresh log to contain %q, got %s", want, output)
		}
	}
}

func newTestAppService(repo appRepository, cache redis.UniversalClient) *AppService {
	return newTestAppServiceWithLogger(repo, cache, log.New(io.Discard, "", 0))
}

func newTestAppServiceWithLogger(repo appRepository, cache redis.UniversalClient, logger *log.Logger) *AppService {
	return &AppService{
		repo:   repo,
		redis:  cache,
		audit:  audit.NewService(nil),
		logger: logger,
	}
}

func testApp(id int64, name string, secret string) domain.App {
	base := time.Date(2026, 6, 1, 8, 0, 0, 0, time.UTC)
	return domain.App{
		ID:             id,
		Name:           name,
		Secret:         secret,
		Remark:         name + " remark",
		Disabled:       false,
		CreateTime:     base.Add(time.Duration(id) * time.Minute),
		LastUpdateTime: base.Add(time.Duration(id) * time.Minute),
	}
}

func stringPtr(value string) *string {
	return &value
}

type fakeAppRepository struct {
	nextID               int64
	apps                 map[int64]domain.App
	listEnabledAppsCalls int
	hardDeleteContextErr error
	onListEnabledApps    func()
	onUpdateApp          func()
}

func (r *fakeAppRepository) ListApps(_ context.Context, _ *int64, _ string, page int, pageSize int, includeDisabled bool) (domain.Page[domain.App], error) {
	apps, err := r.listApps(includeDisabled)
	if err != nil {
		return domain.Page[domain.App]{}, err
	}
	return domain.Page[domain.App]{
		Items:    apps,
		Page:     page,
		PageSize: pageSize,
		Total:    int64(len(apps)),
	}, nil
}

func (r *fakeAppRepository) ListEnabledApps(_ context.Context) ([]domain.App, error) {
	r.listEnabledAppsCalls++
	if r.onListEnabledApps != nil {
		hook := r.onListEnabledApps
		r.onListEnabledApps = nil
		hook()
	}
	return r.listApps(false)
}

func (r *fakeAppRepository) CreateApp(_ context.Context, app domain.App) (domain.App, error) {
	r.ensureStore()
	if r.nextID <= 0 {
		r.nextID = 1
	}
	app.ID = r.nextID
	r.nextID++
	r.apps[app.ID] = app
	return app, nil
}

func (r *fakeAppRepository) UpdateApp(_ context.Context, app domain.App, expectedLastUpdateTime time.Time) (domain.App, error) {
	r.ensureStore()
	if r.onUpdateApp != nil {
		hook := r.onUpdateApp
		r.onUpdateApp = nil
		hook()
	}
	current, ok := r.apps[app.ID]
	if !ok {
		return domain.App{}, repository.ErrNotFound
	}
	if !current.LastUpdateTime.Equal(expectedLastUpdateTime) {
		return domain.App{}, repository.ErrConflict
	}
	r.apps[app.ID] = app
	return app, nil
}

func (r *fakeAppRepository) DeleteApp(_ context.Context, appID int64, expectedLastUpdateTime time.Time) error {
	r.ensureStore()
	app, ok := r.apps[appID]
	if !ok {
		return repository.ErrNotFound
	}
	if !app.LastUpdateTime.Equal(expectedLastUpdateTime) {
		return repository.ErrConflict
	}
	app.Disabled = true
	app.LastUpdateTime = time.Now().UTC()
	r.apps[appID] = app
	return nil
}

func (r *fakeAppRepository) RestoreApp(_ context.Context, app domain.App, expectedLastUpdateTime time.Time, restoreDisabled bool) error {
	r.ensureStore()
	current, ok := r.apps[app.ID]
	if !ok {
		return repository.ErrNotFound
	}
	if !current.LastUpdateTime.Equal(expectedLastUpdateTime) {
		return repository.ErrConflict
	}
	if !restoreDisabled {
		app.Disabled = current.Disabled
	}
	r.apps[app.ID] = app
	return nil
}

func (r *fakeAppRepository) HardDeleteApp(ctx context.Context, appID int64) error {
	r.hardDeleteContextErr = ctx.Err()
	r.ensureStore()
	if _, ok := r.apps[appID]; !ok {
		return repository.ErrNotFound
	}
	delete(r.apps, appID)
	return nil
}

func (r *fakeAppRepository) GetAppByID(_ context.Context, appID int64) (domain.App, error) {
	r.ensureStore()
	app, ok := r.apps[appID]
	if !ok {
		return domain.App{}, repository.ErrNotFound
	}
	return app, nil
}

func (r *fakeAppRepository) listApps(includeDisabled bool) ([]domain.App, error) {
	r.ensureStore()
	ids := make([]int64, 0, len(r.apps))
	for id, app := range r.apps {
		if !includeDisabled && app.Disabled {
			continue
		}
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i int, j int) bool { return ids[i] < ids[j] })
	items := make([]domain.App, 0, len(ids))
	for _, id := range ids {
		items = append(items, r.apps[id])
	}
	return items, nil
}

func (r *fakeAppRepository) ensureStore() {
	if r.apps == nil {
		r.apps = make(map[int64]domain.App)
	}
}

type fakeAppRedis struct {
	redis.UniversalClient
	hashes                  map[string]map[string]string
	stringValues            map[string]string
	hsetKeys                []string
	setKeys                 []string
	failAfterWriteRemaining map[string]int
	failAfterSetRemaining   map[string]int
	failErr                 error
	hgetErr                 error
}

func (r *fakeAppRedis) HSet(_ context.Context, key string, values ...any) *redis.IntCmd {
	if len(values) != 1 {
		return redis.NewIntResult(0, errors.New("unsupported hset payload"))
	}
	payload, ok := values[0].(map[string]any)
	if !ok {
		return redis.NewIntResult(0, errors.New("unsupported hset value type"))
	}

	r.ensureStore()
	r.hashes[key] = make(map[string]string, len(payload))
	for field, value := range payload {
		r.hashes[key][field] = fmt.Sprint(value)
	}
	r.hsetKeys = append(r.hsetKeys, key)

	if remaining := r.failAfterWriteRemaining[key]; remaining > 0 {
		r.failAfterWriteRemaining[key] = remaining - 1
		return redis.NewIntResult(0, r.failErr)
	}
	return redis.NewIntResult(int64(len(payload)), nil)
}

func (r *fakeAppRedis) Del(_ context.Context, keys ...string) *redis.IntCmd {
	r.ensureStore()
	for _, key := range keys {
		delete(r.hashes, key)
		delete(r.stringValues, key)
	}
	return redis.NewIntResult(int64(len(keys)), nil)
}

func (r *fakeAppRedis) Get(_ context.Context, key string) *redis.StringCmd {
	r.ensureStore()
	value, ok := r.stringValues[key]
	if !ok {
		return redis.NewStringResult("", redis.Nil)
	}
	return redis.NewStringResult(value, nil)
}

func (r *fakeAppRedis) Set(_ context.Context, key string, value any, _ time.Duration) *redis.StatusCmd {
	r.ensureStore()
	if remaining := r.failAfterSetRemaining[key]; remaining > 0 {
		r.failAfterSetRemaining[key] = remaining - 1
		return redis.NewStatusResult("", r.failErr)
	}
	r.stringValues[key] = fmt.Sprint(value)
	r.setKeys = append(r.setKeys, key)
	return redis.NewStatusResult("OK", nil)
}

func (r *fakeAppRedis) HGetAll(_ context.Context, key string) *redis.MapStringStringCmd {
	if r.hgetErr != nil {
		return redis.NewMapStringStringResult(nil, r.hgetErr)
	}
	r.ensureStore()
	values := make(map[string]string, len(r.hashes[key]))
	for field, value := range r.hashes[key] {
		values[field] = value
	}
	return redis.NewMapStringStringResult(values, nil)
}

func (r *fakeAppRedis) hashValue(key string, field string) string {
	if r.hashes == nil {
		return ""
	}
	return r.hashes[key][field]
}

func (r *fakeAppRedis) ensureStore() {
	if r.hashes == nil {
		r.hashes = make(map[string]map[string]string)
	}
	if r.stringValues == nil {
		r.stringValues = make(map[string]string)
	}
	if r.failAfterWriteRemaining == nil {
		r.failAfterWriteRemaining = make(map[string]int)
	}
	if r.failAfterSetRemaining == nil {
		r.failAfterSetRemaining = make(map[string]int)
	}
}

func assertCachedAppAuthSummaries(t *testing.T, raw string, want []AppAuthSummary) {
	t.Helper()
	var got []AppAuthSummary
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("decode cached summaries: %v raw=%q", err, raw)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected cached summaries: %#v", got)
	}
}

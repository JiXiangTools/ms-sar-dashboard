package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/JiXiangTools/ms-sar-dashboard/internal/audit"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/domain"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/repository"
)

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
	if !reflect.DeepEqual(cache.hsetKeys, []string{appAuthKey(1), appAuthKey(2), appAuthKey(2)}) {
		t.Fatalf("expected rollback to rewrite the failed app auth key, got %#v", cache.hsetKeys)
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
	nextID int64
	apps   map[int64]domain.App
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

func (r *fakeAppRepository) UpdateApp(_ context.Context, app domain.App) (domain.App, error) {
	r.ensureStore()
	if _, ok := r.apps[app.ID]; !ok {
		return domain.App{}, repository.ErrNotFound
	}
	r.apps[app.ID] = app
	return app, nil
}

func (r *fakeAppRepository) DeleteApp(_ context.Context, appID int64) error {
	r.ensureStore()
	app, ok := r.apps[appID]
	if !ok {
		return repository.ErrNotFound
	}
	app.Disabled = true
	app.LastUpdateTime = time.Now().UTC()
	r.apps[appID] = app
	return nil
}

func (r *fakeAppRepository) RestoreApp(_ context.Context, app domain.App) error {
	r.ensureStore()
	r.apps[app.ID] = app
	return nil
}

func (r *fakeAppRepository) HardDeleteApp(_ context.Context, appID int64) error {
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
	hsetKeys                []string
	failAfterWriteRemaining map[string]int
	failErr                 error
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
	}
	return redis.NewIntResult(int64(len(keys)), nil)
}

func (r *fakeAppRedis) HGetAll(_ context.Context, key string) *redis.MapStringStringCmd {
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
	if r.failAfterWriteRemaining == nil {
		r.failAfterWriteRemaining = make(map[string]int)
	}
}

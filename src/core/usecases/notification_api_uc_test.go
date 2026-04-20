package usecases

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

const testNotificationID = "11111111-1111-4111-8111-111111111111"

type fakeNotificationDBDriver struct {
	mu            sync.Mutex
	listRows      [][]driver.Value
	unreadCount   int64
	markReadRow   []driver.Value
	markAllCount  int64
	clearAllCount int64
	lastArgsByOp  map[string][]driver.NamedValue
}

func (d *fakeNotificationDBDriver) Open(_ string) (driver.Conn, error) {
	return &fakeNotificationDBConn{driver: d}, nil
}

func (d *fakeNotificationDBDriver) setArgs(op string, args []driver.NamedValue) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.lastArgsByOp == nil {
		d.lastArgsByOp = make(map[string][]driver.NamedValue)
	}
	d.lastArgsByOp[op] = append([]driver.NamedValue(nil), args...)
}

func (d *fakeNotificationDBDriver) argAsString(op string, index int) string {
	d.mu.Lock()
	defer d.mu.Unlock()
	args := d.lastArgsByOp[op]
	if len(args) <= index {
		return ""
	}
	value, _ := args[index].Value.(string)
	return value
}

func (d *fakeNotificationDBDriver) cloneListRows() [][]driver.Value {
	d.mu.Lock()
	defer d.mu.Unlock()
	rows := make([][]driver.Value, 0, len(d.listRows))
	for _, row := range d.listRows {
		rows = append(rows, append([]driver.Value(nil), row...))
	}
	return rows
}

func (d *fakeNotificationDBDriver) cloneMarkReadRow() []driver.Value {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.markReadRow == nil {
		return nil
	}
	return append([]driver.Value(nil), d.markReadRow...)
}

func (d *fakeNotificationDBDriver) unreadCountRow() int64 {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.unreadCount
}

type fakeNotificationDBConn struct {
	driver *fakeNotificationDBDriver
}

func (c *fakeNotificationDBConn) Prepare(_ string) (driver.Stmt, error) {
	return nil, errors.New("not implemented")
}

func (c *fakeNotificationDBConn) Close() error { return nil }

func (c *fakeNotificationDBConn) Begin() (driver.Tx, error) {
	return nil, errors.New("not implemented")
}

func (c *fakeNotificationDBConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	normalized := strings.ToLower(query)
	switch {
	case strings.Contains(normalized, "from notifications") && strings.Contains(normalized, "order by created_at desc"):
		c.driver.setArgs("list", args)
		return &fakeNotificationRows{
			values: c.driver.cloneListRows(),
		}, nil
	case strings.Contains(normalized, "with updated as"):
		c.driver.setArgs("read_all", args)
		return &fakeNotificationRows{
			values: [][]driver.Value{{c.driver.markAllCount}},
		}, nil
	case strings.Contains(normalized, "with deleted as"):
		c.driver.setArgs("clear_all", args)
		return &fakeNotificationRows{
			values: [][]driver.Value{{c.driver.clearAllCount}},
		}, nil
	case strings.Contains(normalized, "select count(1)") && strings.Contains(normalized, "from notifications"):
		c.driver.setArgs("count", args)
		return &fakeNotificationRows{
			values: [][]driver.Value{{c.driver.unreadCountRow()}},
		}, nil
	case strings.Contains(normalized, "update notifications"):
		c.driver.setArgs("mark", args)
		row := c.driver.cloneMarkReadRow()
		if row == nil {
			return &fakeNotificationRows{values: [][]driver.Value{}}, nil
		}
		return &fakeNotificationRows{
			values: [][]driver.Value{row},
		}, nil
	default:
		return nil, fmt.Errorf("unexpected query: %s", query)
	}
}

type fakeNotificationRows struct {
	index  int
	values [][]driver.Value
}

func (r *fakeNotificationRows) Columns() []string {
	if len(r.values) == 0 {
		return []string{}
	}
	return make([]string, len(r.values[0]))
}

func (r *fakeNotificationRows) Close() error { return nil }

func (r *fakeNotificationRows) Next(dest []driver.Value) error {
	if r.index >= len(r.values) {
		return io.EOF
	}
	copy(dest, r.values[r.index])
	r.index += 1
	return nil
}

func newTestNotificationHandler(t *testing.T, issuer TokenIssuer) (*NotificationHandler, *fakeNotificationDBDriver) {
	t.Helper()

	driverName := fmt.Sprintf("fake-notification-db-%d", time.Now().UnixNano())
	dbDriver := &fakeNotificationDBDriver{}
	sql.Register(driverName, dbDriver)

	db, err := sql.Open(driverName, "")
	if err != nil {
		t.Fatalf("open fake db: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	return NewNotificationHandler(db, issuer), dbDriver
}

type notificationTestEnvelope struct {
	Success bool            `json:"success"`
	Data    json.RawMessage `json:"data"`
	Error   string          `json:"error"`
}

func decodeNotificationEnvelope(t *testing.T, raw string) notificationTestEnvelope {
	t.Helper()

	envelope := notificationTestEnvelope{}
	if err := json.Unmarshal([]byte(raw), &envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	return envelope
}

func TestListNotificationsCanonicalizesUserAddress(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler, dbDriver := newTestNotificationHandler(t, fakeTokenIssuer{address: testInitAddress})
	now := time.Now().UTC()
	dbDriver.listRows = [][]driver.Value{
		{
			testNotificationID,
			strings.ToLower(testEVMAddress),
			"GROUP_INVITE",
			"Group invite",
			"You were invited to join Alpha",
			"11",
			"/group/11",
			false,
			now,
		},
	}

	ctx, recorder := newJSONContext(http.MethodGet, "/v1/notifications?limit=1", "")
	ctx.Request.Header.Set("Authorization", "Bearer test-token")

	handler.ListNotifications(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", recorder.Code, recorder.Body.String())
	}

	envelope := decodeNotificationEnvelope(t, recorder.Body.String())
	if !envelope.Success {
		t.Fatalf("expected success response, got error %s", envelope.Error)
	}

	var payload notificationListEnvelope
	if err := json.Unmarshal(envelope.Data, &payload); err != nil {
		t.Fatalf("decode notifications: %v", err)
	}
	items := payload.Items
	if len(items) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(items))
	}
	if payload.NextCursor == "" {
		t.Fatalf("expected non-empty next cursor for full page")
	}

	expectedAddress := strings.ToLower(testEVMAddress)
	if got := dbDriver.argAsString("list", 0); got != expectedAddress {
		t.Fatalf("expected canonical address %s, got %s", expectedAddress, got)
	}
}

func TestUnreadCountAndMarkRead(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler, dbDriver := newTestNotificationHandler(t, fakeTokenIssuer{address: strings.ToUpper(testEVMAddress)})
	now := time.Now().UTC()
	dbDriver.unreadCount = 3
	dbDriver.markReadRow = []driver.Value{
		testNotificationID,
		strings.ToLower(testEVMAddress),
		"FUNDING_REMINDER",
		"Funding reminder",
		"It is time to contribute to Alpha.",
		"99",
		"/group/99?tab=deposit",
		true,
		now,
	}

	countCtx, countRecorder := newJSONContext(http.MethodGet, "/v1/notifications/unread-count", "")
	countCtx.Request.Header.Set("Authorization", "Bearer test-token")
	handler.UnreadCount(countCtx)

	if countRecorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", countRecorder.Code, countRecorder.Body.String())
	}
	countEnvelope := decodeNotificationEnvelope(t, countRecorder.Body.String())
	if !countEnvelope.Success {
		t.Fatalf("expected success response, got error %s", countEnvelope.Error)
	}
	var countPayload notificationUnreadCount
	if err := json.Unmarshal(countEnvelope.Data, &countPayload); err != nil {
		t.Fatalf("decode unread count payload: %v", err)
	}
	if countPayload.Count != 3 {
		t.Fatalf("expected unread count 3, got %d", countPayload.Count)
	}

	markCtx, markRecorder := newJSONContext(http.MethodPatch, "/v1/notifications/"+testNotificationID+"/read", "")
	markCtx.Params = gin.Params{{Key: "id", Value: testNotificationID}}
	markCtx.Request.Header.Set("Authorization", "Bearer test-token")
	handler.MarkRead(markCtx)

	if markRecorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", markRecorder.Code, markRecorder.Body.String())
	}
	markEnvelope := decodeNotificationEnvelope(t, markRecorder.Body.String())
	if !markEnvelope.Success {
		t.Fatalf("expected success response, got error %s", markEnvelope.Error)
	}

	var item notificationItem
	if err := json.Unmarshal(markEnvelope.Data, &item); err != nil {
		t.Fatalf("decode mark-read item: %v", err)
	}
	if !item.IsRead {
		t.Fatalf("expected notification to be marked as read")
	}

	expectedAddress := strings.ToLower(testEVMAddress)
	if got := dbDriver.argAsString("count", 0); got != expectedAddress {
		t.Fatalf("expected count query canonical address %s, got %s", expectedAddress, got)
	}
	if got := dbDriver.argAsString("mark", 1); got != expectedAddress {
		t.Fatalf("expected mark query canonical address %s, got %s", expectedAddress, got)
	}
}

func TestMarkReadAll(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler, dbDriver := newTestNotificationHandler(t, fakeTokenIssuer{address: testInitAddress})
	dbDriver.markAllCount = 4

	readAllCtx, readAllRecorder := newJSONContext(http.MethodPatch, "/v1/notifications/read-all", "")
	readAllCtx.Request.Header.Set("Authorization", "Bearer test-token")

	handler.MarkReadAll(readAllCtx)

	if readAllRecorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", readAllRecorder.Code, readAllRecorder.Body.String())
	}

	envelope := decodeNotificationEnvelope(t, readAllRecorder.Body.String())
	if !envelope.Success {
		t.Fatalf("expected success response, got error %s", envelope.Error)
	}

	var payload notificationReadAllResult
	if err := json.Unmarshal(envelope.Data, &payload); err != nil {
		t.Fatalf("decode read-all payload: %v", err)
	}
	if payload.UpdatedCount != 4 {
		t.Fatalf("expected updated count 4, got %d", payload.UpdatedCount)
	}

	expectedAddress := strings.ToLower(testEVMAddress)
	if got := dbDriver.argAsString("read_all", 0); got != expectedAddress {
		t.Fatalf("expected read-all canonical address %s, got %s", expectedAddress, got)
	}
}

func TestClearAll(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler, dbDriver := newTestNotificationHandler(t, fakeTokenIssuer{address: testInitAddress})
	dbDriver.clearAllCount = 7

	clearCtx, clearRecorder := newJSONContext(http.MethodDelete, "/v1/notifications", "")
	clearCtx.Request.Header.Set("Authorization", "Bearer test-token")

	handler.ClearAll(clearCtx)

	if clearRecorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", clearRecorder.Code, clearRecorder.Body.String())
	}

	envelope := decodeNotificationEnvelope(t, clearRecorder.Body.String())
	if !envelope.Success {
		t.Fatalf("expected success response, got error %s", envelope.Error)
	}

	var payload notificationClearAllResult
	if err := json.Unmarshal(envelope.Data, &payload); err != nil {
		t.Fatalf("decode clear-all payload: %v", err)
	}
	if payload.DeletedCount != 7 {
		t.Fatalf("expected deleted count 7, got %d", payload.DeletedCount)
	}

	expectedAddress := strings.ToLower(testEVMAddress)
	if got := dbDriver.argAsString("clear_all", 0); got != expectedAddress {
		t.Fatalf("expected clear-all canonical address %s, got %s", expectedAddress, got)
	}
}

func TestNotificationCursorRoundTrip(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	expected := notificationCursor{
		CreatedAt: now,
		ID:        testNotificationID,
	}
	encoded := encodeNotificationCursor(expected)
	if strings.TrimSpace(encoded) == "" {
		t.Fatalf("expected encoded cursor")
	}

	decoded, err := decodeNotificationCursor(encoded)
	if err != nil {
		t.Fatalf("decode cursor: %v", err)
	}
	if decoded == nil {
		t.Fatalf("expected decoded cursor")
	}
	if !decoded.CreatedAt.Equal(now) {
		t.Fatalf("expected created_at %s, got %s", now, decoded.CreatedAt)
	}
	if decoded.ID != expected.ID {
		t.Fatalf("expected id %s, got %s", expected.ID, decoded.ID)
	}
}

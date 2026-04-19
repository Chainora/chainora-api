package handler

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/gin-gonic/gin"
)

const (
	testEVMAddress  = "0x1111111111111111111111111111111111111111"
	testInitAddress = "init1zyg3zyg3zyg3zyg3zyg3zyg3zyg3zyg30th3ed"
)

type fakeTokenIssuer struct {
	address string
	err     error
}

func (f fakeTokenIssuer) GenerateTokenPair(_, _ string) (string, string, error) {
	return "", "", errors.New("not implemented")
}

func (f fakeTokenIssuer) RefreshFromToken(_ string) (string, string, string, error) {
	return "", "", "", errors.New("not implemented")
}

func (f fakeTokenIssuer) ParseAccessToken(_ string) (string, string, error) {
	if f.err != nil {
		return "", "", f.err
	}
	return "test-session", f.address, nil
}

type fakeGroupDBDriver struct {
	mu       sync.Mutex
	lastArgs []driver.NamedValue
}

func (d *fakeGroupDBDriver) Open(_ string) (driver.Conn, error) {
	return &fakeGroupDBConn{driver: d}, nil
}

func (d *fakeGroupDBDriver) setArgs(args []driver.NamedValue) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.lastArgs = append([]driver.NamedValue(nil), args...)
}

func (d *fakeGroupDBDriver) creatorArg() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.lastArgs) < 3 {
		return ""
	}
	value, _ := d.lastArgs[2].Value.(string)
	return value
}

func (d *fakeGroupDBDriver) hasInsert() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.lastArgs) > 0
}

type fakeGroupDBConn struct {
	driver *fakeGroupDBDriver
}

func (c *fakeGroupDBConn) Prepare(_ string) (driver.Stmt, error) {
	return nil, errors.New("not implemented")
}

func (c *fakeGroupDBConn) Close() error { return nil }

func (c *fakeGroupDBConn) Begin() (driver.Tx, error) {
	return nil, errors.New("not implemented")
}

func (c *fakeGroupDBConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if !strings.Contains(query, "INSERT INTO groups") {
		return nil, fmt.Errorf("unexpected query: %s", query)
	}

	c.driver.setArgs(args)
	now := time.Now().UTC()
	poolID, _ := args[0].Value.(string)
	poolAddress, _ := args[1].Value.(string)
	creatorAddress, _ := args[2].Value.(string)
	name, _ := args[3].Value.(string)
	description, _ := args[4].Value.(string)
	groupImageURL, _ := args[5].Value.(string)
	publicRecruitment, _ := args[6].Value.(bool)
	contributionAmount, _ := args[7].Value.(string)
	targetMembers, _ := args[8].Value.(int64)
	periodDuration, _ := args[9].Value.(int64)
	contributionWindow, _ := args[10].Value.(int64)
	auctionWindow, _ := args[11].Value.(int64)
	txHash, _ := args[12].Value.(string)

	values := []driver.Value{
		poolID,
		poolAddress,
		creatorAddress,
		name,
		description,
		groupImageURL,
		publicRecruitment,
		contributionAmount,
		targetMembers,
		periodDuration,
		contributionWindow,
		auctionWindow,
		int64(0),
		int64(0),
		"0",
		"0",
		int64(1),
		false,
		txHash,
		nil,
		now,
		now,
	}

	return &fakeGroupRows{
		columns: make([]string, len(values)),
		values:  values,
	}, nil
}

type fakeGroupRows struct {
	columns []string
	values  []driver.Value
	read    bool
}

func (r *fakeGroupRows) Columns() []string { return r.columns }

func (r *fakeGroupRows) Close() error { return nil }

func (r *fakeGroupRows) Next(dest []driver.Value) error {
	if r.read {
		return io.EOF
	}

	copy(dest, r.values)
	r.read = true
	return nil
}

func newTestGroupHandler(t *testing.T, issuer TokenIssuer) (*GroupHandler, *fakeGroupDBDriver) {
	t.Helper()

	driverName := fmt.Sprintf("fake-group-db-%d", time.Now().UnixNano())
	dbDriver := &fakeGroupDBDriver{}
	sql.Register(driverName, dbDriver)

	db, err := sql.Open(driverName, "")
	if err != nil {
		t.Fatalf("open fake db: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	return NewGroupHandler(db, issuer, ""), dbDriver
}

func newJSONContext(method, path string, body string) (*gin.Context, *httptest.ResponseRecorder) {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	ctx.Request = req
	return ctx, recorder
}

func TestCanonicalizeTokenAddressToEVM(t *testing.T) {
	t.Run("accepts evm hex address", func(t *testing.T) {
		got, err := canonicalizeTokenAddressToEVM(strings.ToUpper(testEVMAddress))
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if got != common.HexToAddress(testEVMAddress).Hex() {
			t.Fatalf("unexpected canonical address: %s", got)
		}
	})

	t.Run("converts init bech32 address", func(t *testing.T) {
		got, err := canonicalizeTokenAddressToEVM(testInitAddress)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if got != common.HexToAddress(testEVMAddress).Hex() {
			t.Fatalf("unexpected canonical address: %s", got)
		}
	})

	t.Run("rejects invalid address", func(t *testing.T) {
		if _, err := canonicalizeTokenAddressToEVM("not-an-address"); err == nil {
			t.Fatalf("expected error for invalid address")
		}
	})
}

func TestCreateGroupCanonicalizesCreatorAddressFromToken(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name         string
		tokenAddress string
		expectedEVM  string
	}{
		{
			name:         "token with init address",
			tokenAddress: testInitAddress,
			expectedEVM:  testEVMAddress,
		},
		{
			name:         "token with evm address",
			tokenAddress: strings.ToUpper(testEVMAddress),
			expectedEVM:  testEVMAddress,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			handler, dbDriver := newTestGroupHandler(t, fakeTokenIssuer{address: tc.tokenAddress})
			ctx, recorder := newJSONContext(http.MethodPost, "/v1/groups", `{
				"poolId": "1",
				"poolAddress": "0x0000000000000000000000000000000000000001",
				"name": "Test Group",
				"description": "group from test",
				"groupImageUrl": "",
				"publicRecruitment": true,
				"contributionAmount": "1000000000000000000",
				"targetMembers": 10,
				"periodDuration": 86400,
				"contributionWindow": 3600,
				"auctionWindow": 1800,
				"txHash": "0xabc"
			}`)
			ctx.Request.Header.Set("Authorization", "Bearer test-token")

			handler.CreateGroup(ctx)

			if recorder.Code != http.StatusOK {
				t.Fatalf("expected status 200, got %d with body %s", recorder.Code, recorder.Body.String())
			}

			expectedLower := strings.ToLower(common.HexToAddress(tc.expectedEVM).Hex())
			if gotCreator := dbDriver.creatorArg(); gotCreator != expectedLower {
				t.Fatalf("expected creator address %s, got %s", expectedLower, gotCreator)
			}

			var payload struct {
				Success bool `json:"success"`
				Data    struct {
					CreatorAddress string `json:"creatorAddress"`
				} `json:"data"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
				t.Fatalf("decode response body: %v", err)
			}
			if !payload.Success {
				t.Fatalf("expected success response, got %s", recorder.Body.String())
			}
			if payload.Data.CreatorAddress != expectedLower {
				t.Fatalf("expected response creatorAddress %s, got %s", expectedLower, payload.Data.CreatorAddress)
			}
		})
	}
}

func TestCreateGroupRejectsTargetMembersBelowThree(t *testing.T) {
	gin.SetMode(gin.TestMode)

	handler, dbDriver := newTestGroupHandler(t, fakeTokenIssuer{address: testEVMAddress})
	ctx, recorder := newJSONContext(http.MethodPost, "/v1/groups", `{
		"poolId": "1",
		"poolAddress": "0x0000000000000000000000000000000000000001",
		"name": "Test Group",
		"description": "group from test",
		"groupImageUrl": "",
		"publicRecruitment": true,
		"contributionAmount": "1000000000000000000",
		"targetMembers": 2,
		"periodDuration": 86400,
		"contributionWindow": 3600,
		"auctionWindow": 1800,
		"txHash": "0xabc"
	}`)
	ctx.Request.Header.Set("Authorization", "Bearer test-token")

	handler.CreateGroup(ctx)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d with body %s", recorder.Code, recorder.Body.String())
	}
	if dbDriver.hasInsert() {
		t.Fatalf("expected create group to fail before insert")
	}
	if !strings.Contains(strings.ToLower(recorder.Body.String()), "targetmembers") {
		t.Fatalf("expected targetMembers validation error, got %s", recorder.Body.String())
	}
}

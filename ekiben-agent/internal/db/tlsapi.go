package db

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

type APIClient struct {
	baseURL string
	token   string
	http    *http.Client
}

func NewAPIClient(baseURL, token string) (*APIClient, error) {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		return nil, errors.New("api base url is required")
	}
	return &APIClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   strings.TrimSpace(token),
		http:    &http.Client{Timeout: 15 * time.Second},
	}, nil
}

func (c *APIClient) QueryNamed(ctx context.Context, name string, args []any, allowWrite bool) (map[string]any, error) {
	switch name {
	case "get_user_by_baid":
		baid, err := intArg(args, 0, "baid")
		if err != nil {
			return nil, err
		}
		var user map[string]any
		if err := c.doJSON(ctx, http.MethodGet, fmt.Sprintf("/api/Users/%d", baid), nil, &user); err != nil {
			return nil, err
		}
		if user == nil {
			return map[string]any{"rows": []map[string]any{}}, nil
		}
		return map[string]any{"rows": []map[string]any{user}}, nil
	case "list_cards":
		rows, err := c.listCards(ctx)
		if err != nil {
			return nil, err
		}
		return map[string]any{"rows": rows}, nil
	case "list_song_best_by_baid":
		baid, err := intArg(args, 0, "baid")
		if err != nil {
			return nil, err
		}
		limit := -1
		if len(args) > 1 {
			lim, err := anyToInt(args[1])
			if err == nil {
				limit = lim
			}
		}
		rows, err := c.songBestRows(ctx, baid)
		if err != nil {
			return nil, err
		}
		if limit >= 0 && limit < len(rows) {
			rows = rows[:limit]
		}
		return map[string]any{"rows": rows}, nil
	case "update_user_name":
		if !allowWrite {
			return nil, errors.New("write queries disabled")
		}
		return nil, errors.New("query not supported in api mode: update_user_name")
	default:
		return nil, fmt.Errorf("unknown query: %s", name)
	}
}

func (c *APIClient) TableSelect(ctx context.Context, table string, columns []string, filters map[string]any, orderBy []OrderBy, limit *int, offset *int) (map[string]any, error) {
	if len(orderBy) > 0 {
		return nil, errors.New("orderBy is not supported in api mode")
	}

	rows, err := c.tableRows(ctx, table, filters)
	if err != nil {
		return nil, err
	}

	rows = applyFilterRows(rows, filters)
	rows = applyColumnProjection(rows, columns)
	rows = applyOffsetLimit(rows, offset, limit)
	return map[string]any{"rows": rows}, nil
}

func (c *APIClient) TableInsert(ctx context.Context, table string, values map[string]any, allowWrite bool) (map[string]any, error) {
	if !allowWrite {
		return nil, errors.New("write queries disabled")
	}

	switch table {
	case "Card":
		baid, ok := anyToIntNoError(values["Baid"])
		if !ok {
			return nil, errors.New("Card insert requires Baid")
		}
		accessCode := strings.TrimSpace(fmt.Sprintf("%v", values["AccessCode"]))
		if accessCode == "" {
			return nil, errors.New("Card insert requires AccessCode")
		}
		body := map[string]any{"baid": baid, "accessCode": accessCode}
		if err := c.doJSON(ctx, http.MethodPost, "/api/Cards/BindAccessCode", body, nil); err != nil {
			return nil, err
		}
		return map[string]any{"rowsAffected": int64(1), "lastInsertId": int64(0)}, nil
	default:
		return nil, fmt.Errorf("table.insert not supported in api mode for table: %s", table)
	}
}

func (c *APIClient) TableUpdate(ctx context.Context, table string, values map[string]any, filters map[string]any, allowWrite bool) (map[string]any, error) {
	if !allowWrite {
		return nil, errors.New("write queries disabled")
	}
	return nil, fmt.Errorf("table.update not supported in api mode for table: %s", table)
}

func (c *APIClient) TableDelete(ctx context.Context, table string, filters map[string]any, allowWrite bool) (map[string]any, error) {
	if !allowWrite {
		return nil, errors.New("write queries disabled")
	}

	switch table {
	case "Card":
		accessCode := strings.TrimSpace(fmt.Sprintf("%v", filters["AccessCode"]))
		if accessCode == "" {
			return nil, errors.New("Card delete requires AccessCode filter")
		}
		path := "/api/Cards/" + url.PathEscape(accessCode)
		if err := c.doJSON(ctx, http.MethodDelete, path, nil, nil); err != nil {
			return nil, err
		}
		return map[string]any{"rowsAffected": int64(1)}, nil
	default:
		return nil, fmt.Errorf("table.delete not supported in api mode for table: %s", table)
	}
}

func (c *APIClient) tableRows(ctx context.Context, table string, filters map[string]any) ([]map[string]any, error) {
	switch table {
	case "UserData":
		baid, ok := filterInt(filters, "Baid")
		if !ok {
			return nil, errors.New("UserData table.select requires Baid filter in api mode")
		}
		var user map[string]any
		if err := c.doJSON(ctx, http.MethodGet, fmt.Sprintf("/api/UserSettings/%d", baid), nil, &user); err != nil {
			return nil, err
		}
		if user == nil {
			return []map[string]any{}, nil
		}
		return []map[string]any{user}, nil
	case "SongBestData":
		baid, ok := filterInt(filters, "Baid")
		if !ok {
			return nil, errors.New("SongBestData table.select requires Baid filter in api mode")
		}
		return c.songBestRows(ctx, baid)
	case "SongPlayData":
		baid, ok := filterInt(filters, "Baid")
		if !ok {
			return nil, errors.New("SongPlayData table.select requires Baid filter in api mode")
		}
		return c.playHistoryRows(ctx, baid)
	case "Card":
		if baid, ok := filterInt(filters, "Baid"); ok {
			return c.cardsByBaid(ctx, baid)
		}
		return c.listCards(ctx)
	default:
		return nil, fmt.Errorf("table.select not supported in api mode for table: %s", table)
	}
}

func (c *APIClient) songBestRows(ctx context.Context, baid int) ([]map[string]any, error) {
	var payload struct {
		SongBestData []map[string]any `json:"songBestData"`
	}
	if err := c.doJSON(ctx, http.MethodGet, fmt.Sprintf("/api/PlayData/%d", baid), nil, &payload); err != nil {
		return nil, err
	}
	if payload.SongBestData == nil {
		return []map[string]any{}, nil
	}
	return payload.SongBestData, nil
}

func (c *APIClient) playHistoryRows(ctx context.Context, baid int) ([]map[string]any, error) {
	var payload struct {
		SongHistoryData []map[string]any `json:"songHistoryData"`
	}
	if err := c.doJSON(ctx, http.MethodGet, fmt.Sprintf("/api/PlayHistory/%d", baid), nil, &payload); err != nil {
		return nil, err
	}
	if payload.SongHistoryData == nil {
		return []map[string]any{}, nil
	}
	return payload.SongHistoryData, nil
}

func (c *APIClient) listCards(ctx context.Context) ([]map[string]any, error) {
	rows := make([]map[string]any, 0)
	page := 1

	for {
		path := fmt.Sprintf("/api/Users?page=%d&limit=200", page)
		var payload struct {
			Users []struct {
				BAID        int      `json:"baid"`
				AccessCodes []string `json:"accessCodes"`
			} `json:"users"`
			TotalPages int `json:"totalPages"`
		}
		if err := c.doJSON(ctx, http.MethodGet, path, nil, &payload); err != nil {
			return nil, err
		}

		for _, user := range payload.Users {
			for _, accessCode := range user.AccessCodes {
				rows = append(rows, map[string]any{
					"AccessCode": accessCode,
					"Baid":       user.BAID,
				})
			}
		}

		if payload.TotalPages <= 0 || page >= payload.TotalPages {
			break
		}
		page++
	}

	sort.Slice(rows, func(i, j int) bool {
		bi, _ := anyToInt(rows[i]["Baid"])
		bj, _ := anyToInt(rows[j]["Baid"])
		if bi != bj {
			return bi < bj
		}
		return fmt.Sprintf("%v", rows[i]["AccessCode"]) < fmt.Sprintf("%v", rows[j]["AccessCode"])
	})

	return rows, nil
}

func (c *APIClient) cardsByBaid(ctx context.Context, baid int) ([]map[string]any, error) {
	var payload struct {
		BAID        int      `json:"baid"`
		AccessCodes []string `json:"accessCodes"`
	}
	if err := c.doJSON(ctx, http.MethodGet, fmt.Sprintf("/api/Users/%d", baid), nil, &payload); err != nil {
		return nil, err
	}
	rows := make([]map[string]any, 0, len(payload.AccessCodes))
	for _, accessCode := range payload.AccessCodes {
		rows = append(rows, map[string]any{"AccessCode": accessCode, "Baid": baid})
	}
	return rows, nil
}

func (c *APIClient) doJSON(ctx context.Context, method, path string, body any, out any) error {
	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		bodyReader = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bodyReader)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		if len(msg) == 0 {
			return fmt.Errorf("tls api error: %s", resp.Status)
		}
		return fmt.Errorf("tls api error: %s: %s", resp.Status, strings.TrimSpace(string(msg)))
	}

	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func intArg(args []any, idx int, name string) (int, error) {
	if idx >= len(args) {
		return 0, fmt.Errorf("missing arg: %s", name)
	}
	return anyToInt(args[idx])
}

func anyToInt(value any) (int, error) {
	switch v := value.(type) {
	case int:
		return v, nil
	case int64:
		return int(v), nil
	case float64:
		return int(v), nil
	case string:
		i, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil {
			return 0, fmt.Errorf("invalid int: %v", value)
		}
		return i, nil
	default:
		return 0, fmt.Errorf("invalid int: %v", value)
	}
}

func anyToIntNoError(value any) (int, bool) {
	i, err := anyToInt(value)
	if err != nil {
		return 0, false
	}
	return i, true
}

func filterInt(filters map[string]any, key string) (int, bool) {
	if len(filters) == 0 {
		return 0, false
	}
	value, ok := filters[key]
	if !ok {
		return 0, false
	}
	i, err := anyToInt(value)
	if err != nil {
		return 0, false
	}
	return i, true
}

func applyFilterRows(rows []map[string]any, filters map[string]any) []map[string]any {
	if len(filters) == 0 {
		return rows
	}
	result := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		ok := true
		for key, expected := range filters {
			if fmt.Sprintf("%v", row[key]) != fmt.Sprintf("%v", expected) {
				ok = false
				break
			}
		}
		if ok {
			result = append(result, row)
		}
	}
	return result
}

func applyColumnProjection(rows []map[string]any, columns []string) []map[string]any {
	if len(columns) == 0 {
		return rows
	}
	result := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		item := make(map[string]any, len(columns))
		for _, column := range columns {
			item[column] = row[column]
		}
		result = append(result, item)
	}
	return result
}

func applyOffsetLimit(rows []map[string]any, offset *int, limit *int) []map[string]any {
	start := 0
	if offset != nil && *offset > 0 {
		start = *offset
	}
	if start >= len(rows) {
		return []map[string]any{}
	}
	rows = rows[start:]
	if limit != nil && *limit >= 0 && *limit < len(rows) {
		rows = rows[:*limit]
	}
	return rows
}

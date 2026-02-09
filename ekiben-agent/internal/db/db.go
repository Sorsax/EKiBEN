package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type Query struct {
	SQL      string
	ReadOnly bool
}

var Queries = map[string]Query{
	"get_user_by_baid": {
		SQL:      "SELECT * FROM UserData WHERE Baid = ? LIMIT 1",
		ReadOnly: true,
	},
	"list_cards": {
		SQL:      "SELECT AccessCode, Baid FROM Card ORDER BY Baid",
		ReadOnly: true,
	},
	"list_song_best_by_baid": {
		SQL:      "SELECT * FROM SongBestData WHERE Baid = ? LIMIT ?",
		ReadOnly: true,
	},
	"update_user_name": {
		SQL:      "UPDATE UserData SET MyDonName = ? WHERE Baid = ?",
		ReadOnly: false,
	},
}

var TableSchemas = map[string][]string{
	"UserData": {
		"Baid", "AchievementDisplayDifficulty", "AiWinCount", "ColorBody", "ColorFace", "ColorLimb",
		"CostumeData", "CostumeFlgArray", "DifficultyPlayedArray", "DifficultySettingArray",
		"DisplayAchievement", "DisplayDan", "FavoriteSongsArray", "GenericInfoFlgArray", "IsAdmin",
		"IsSkipOn", "IsVoiceOn", "LastPlayDatetime", "LastPlayMode", "MyDonName", "MyDonNameLanguage",
		"NotesPosition", "OptionSetting", "SelectedToneId", "Title", "TitleFlgArray", "TitlePlateId",
		"ToneFlgArray", "UnlockedSongIdList", "UnlockedBody", "UnlockedFace", "UnlockedHead",
		"UnlockedKigurumi", "UnlockedPuchi", "CurrentBody", "CurrentFace", "CurrentHead",
		"CurrentKigurumi", "CurrentPuchi", "DifficultyPlayedCourse", "DifficultyPlayedSort",
		"DifficultyPlayedStar", "DifficultySettingCourse", "DifficultySettingSort", "DifficultySettingStar",
		"UnlockedUraSongIdList",
	},
	"Credential": {"Baid", "Password", "Salt"},
	"Card":       {"AccessCode", "Baid"},
	"Tokens":     {"Baid", "Id", "Count"},
	"SongPlayData": {
		"Id", "Baid", "ComboCount", "Crown", "Difficulty", "DrumrollCount", "GoodCount", "HitCount",
		"MissCount", "OkCount", "PlayTime", "Score", "ScoreRank", "ScoreRate", "Skipped", "SongId",
		"SongNumber",
	},
	"SongBestData": {"Baid", "SongId", "Difficulty", "BestCrown", "BestRate", "BestScore", "BestScoreRank"},
	"AiScoreData":  {"Baid", "SongId", "Difficulty", "IsWin"},
	"AiSectionScoreData": {
		"Baid", "SongId", "Difficulty", "SectionIndex", "Crown", "IsWin", "Score", "GoodCount",
		"OkCount", "MissCount", "DrumrollCount",
	},
	"DanScoreData": {"Baid", "DanId", "DanType", "ArrivalSongCount", "ClearState", "ComboCountTotal", "SoulGaugeTotal"},
	"DanStageScoreData": {
		"Baid", "DanId", "DanType", "SongNumber", "BadCount", "ComboCount", "DrumrollCount",
		"GoodCount", "HighScore", "OkCount", "PlayScore", "TotalHitCount",
	},
	"sqlite_sequence":       {"name", "seq"},
	"__EFMigrationsHistory": {"MigrationId", "ProductVersion"},
}

func Open(dbPath string) (*sql.DB, error) {
	if dbPath == "" {
		return nil, errors.New("db path is required")
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}

	db.SetMaxOpenConns(5)
	db.SetMaxIdleConns(2)
	db.SetConnMaxLifetime(2 * time.Minute)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}

	return db, nil
}

func CountUsers(db *sql.DB) (int64, error) {
	var count int64
	row := db.QueryRow("SELECT COUNT(*) FROM UserData")
	if err := row.Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func QueryNamed(ctx context.Context, db *sql.DB, name string, args []any, allowWrite bool) (map[string]any, error) {
	q, ok := Queries[name]
	if !ok {
		return nil, fmt.Errorf("unknown query: %s", name)
	}
	if !q.ReadOnly && !allowWrite {
		return nil, errors.New("write queries disabled")
	}

	normalized := normalizeArgs(args)

	if q.ReadOnly {
		rows, err := db.QueryContext(ctx, q.SQL, normalized...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()

		result, err := rowsToMaps(rows)
		if err != nil {
			return nil, err
		}
		return map[string]any{"rows": result}, nil
	}

	res, err := db.ExecContext(ctx, q.SQL, normalized...)
	if err != nil {
		return nil, err
	}

	affected, _ := res.RowsAffected()
	lastID, _ := res.LastInsertId()
	return map[string]any{"rowsAffected": affected, "lastInsertId": lastID}, nil
}

func TableSelect(ctx context.Context, db *sql.DB, table string, columns []string, filters map[string]any, orderBy []OrderBy, limit *int, offset *int) (map[string]any, error) {
	cols, err := validateTableAndColumns(table, columns)
	if err != nil {
		return nil, err
	}

	selectCols := "*"
	if len(cols) > 0 {
		selectCols = strings.Join(cols, ", ")
	}

	whereSQL, args, err := buildWhere(table, filters)
	if err != nil {
		return nil, err
	}

	orderSQL, err := buildOrderBy(table, orderBy)
	if err != nil {
		return nil, err
	}

	limitSQL := ""
	if limit != nil {
		limitSQL = " LIMIT ?"
		args = append(args, *limit)
	}
	if offset != nil {
		limitSQL += " OFFSET ?"
		args = append(args, *offset)
	}

	query := fmt.Sprintf("SELECT %s FROM %s%s%s%s", selectCols, quoteIdent(table), whereSQL, orderSQL, limitSQL)
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result, err := rowsToMaps(rows)
	if err != nil {
		return nil, err
	}
	return map[string]any{"rows": result}, nil
}

func TableInsert(ctx context.Context, db *sql.DB, table string, values map[string]any, allowWrite bool) (map[string]any, error) {
	if !allowWrite {
		return nil, errors.New("write queries disabled")
	}
	cols, args, err := buildInsert(table, values)
	if err != nil {
		return nil, err
	}

	placeholders := make([]string, len(cols))
	for i := range cols {
		placeholders[i] = "?"
	}

	query := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", quoteIdent(table), strings.Join(cols, ", "), strings.Join(placeholders, ", "))
	res, err := db.ExecContext(ctx, query, normalizeArgs(args)...)
	if err != nil {
		return nil, err
	}
	affected, _ := res.RowsAffected()
	lastID, _ := res.LastInsertId()
	return map[string]any{"rowsAffected": affected, "lastInsertId": lastID}, nil
}

func TableUpdate(ctx context.Context, db *sql.DB, table string, values map[string]any, filters map[string]any, allowWrite bool) (map[string]any, error) {
	if !allowWrite {
		return nil, errors.New("write queries disabled")
	}
	setSQL, setArgs, err := buildSet(table, values)
	if err != nil {
		return nil, err
	}
	whereSQL, whereArgs, err := buildWhere(table, filters)
	if err != nil {
		return nil, err
	}
	if whereSQL == "" {
		return nil, errors.New("update requires filters")
	}

	query := fmt.Sprintf("UPDATE %s SET %s%s", quoteIdent(table), setSQL, whereSQL)
	args := append(setArgs, whereArgs...)
	res, err := db.ExecContext(ctx, query, normalizeArgs(args)...)
	if err != nil {
		return nil, err
	}
	affected, _ := res.RowsAffected()
	return map[string]any{"rowsAffected": affected}, nil
}

func TableDelete(ctx context.Context, db *sql.DB, table string, filters map[string]any, allowWrite bool) (map[string]any, error) {
	if !allowWrite {
		return nil, errors.New("write queries disabled")
	}
	whereSQL, args, err := buildWhere(table, filters)
	if err != nil {
		return nil, err
	}
	if whereSQL == "" {
		return nil, errors.New("delete requires filters")
	}

	query := fmt.Sprintf("DELETE FROM %s%s", quoteIdent(table), whereSQL)
	res, err := db.ExecContext(ctx, query, normalizeArgs(args)...)
	if err != nil {
		return nil, err
	}
	affected, _ := res.RowsAffected()
	return map[string]any{"rowsAffected": affected}, nil
}

type OrderBy struct {
	Column string
	Desc   bool
}

func validateTableAndColumns(table string, columns []string) ([]string, error) {
	allowed, ok := TableSchemas[table]
	if !ok {
		return nil, fmt.Errorf("unknown table: %s", table)
	}

	allowedSet := make(map[string]struct{}, len(allowed))
	for _, col := range allowed {
		allowedSet[col] = struct{}{}
	}

	if len(columns) == 0 {
		return nil, nil
	}

	result := make([]string, 0, len(columns))
	for _, col := range columns {
		if _, ok := allowedSet[col]; !ok {
			return nil, fmt.Errorf("unknown column: %s", col)
		}
		result = append(result, quoteIdent(col))
	}
	return result, nil
}

func buildWhere(table string, filters map[string]any) (string, []any, error) {
	if len(filters) == 0 {
		return "", nil, nil
	}

	allowed, ok := TableSchemas[table]
	if !ok {
		return "", nil, fmt.Errorf("unknown table: %s", table)
	}
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, col := range allowed {
		allowedSet[col] = struct{}{}
	}

	keys := make([]string, 0, len(filters))
	for key := range filters {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	clauses := make([]string, 0, len(keys))
	args := make([]any, 0, len(keys))
	for _, key := range keys {
		if _, ok := allowedSet[key]; !ok {
			return "", nil, fmt.Errorf("unknown column: %s", key)
		}
		clauses = append(clauses, fmt.Sprintf("%s = ?", quoteIdent(key)))
		args = append(args, filters[key])
	}

	return " WHERE " + strings.Join(clauses, " AND "), args, nil
}

func buildOrderBy(table string, orderBy []OrderBy) (string, error) {
	if len(orderBy) == 0 {
		return "", nil
	}
	allowed, ok := TableSchemas[table]
	if !ok {
		return "", fmt.Errorf("unknown table: %s", table)
	}
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, col := range allowed {
		allowedSet[col] = struct{}{}
	}

	parts := make([]string, 0, len(orderBy))
	for _, item := range orderBy {
		if _, ok := allowedSet[item.Column]; !ok {
			return "", fmt.Errorf("unknown column: %s", item.Column)
		}
		direction := "ASC"
		if item.Desc {
			direction = "DESC"
		}
		parts = append(parts, fmt.Sprintf("%s %s", quoteIdent(item.Column), direction))
	}
	return " ORDER BY " + strings.Join(parts, ", "), nil
}

func buildInsert(table string, values map[string]any) ([]string, []any, error) {
	if len(values) == 0 {
		return nil, nil, errors.New("insert requires values")
	}
	allowed, ok := TableSchemas[table]
	if !ok {
		return nil, nil, fmt.Errorf("unknown table: %s", table)
	}
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, col := range allowed {
		allowedSet[col] = struct{}{}
	}

	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	cols := make([]string, 0, len(keys))
	args := make([]any, 0, len(keys))
	for _, key := range keys {
		if _, ok := allowedSet[key]; !ok {
			return nil, nil, fmt.Errorf("unknown column: %s", key)
		}
		cols = append(cols, quoteIdent(key))
		args = append(args, values[key])
	}
	return cols, args, nil
}

func buildSet(table string, values map[string]any) (string, []any, error) {
	if len(values) == 0 {
		return "", nil, errors.New("update requires values")
	}
	allowed, ok := TableSchemas[table]
	if !ok {
		return "", nil, fmt.Errorf("unknown table: %s", table)
	}
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, col := range allowed {
		allowedSet[col] = struct{}{}
	}

	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	args := make([]any, 0, len(keys))
	for _, key := range keys {
		if _, ok := allowedSet[key]; !ok {
			return "", nil, fmt.Errorf("unknown column: %s", key)
		}
		parts = append(parts, fmt.Sprintf("%s = ?", quoteIdent(key)))
		args = append(args, values[key])
	}
	return strings.Join(parts, ", "), args, nil
}

func quoteIdent(name string) string {
	return "\"" + strings.ReplaceAll(name, "\"", "\"\"") + "\""
}

func normalizeArgs(args []any) []any {
	normalized := make([]any, 0, len(args))
	for _, arg := range args {
		switch v := arg.(type) {
		case float64:
			if v == float64(int64(v)) {
				normalized = append(normalized, int64(v))
			} else {
				normalized = append(normalized, v)
			}
		default:
			normalized = append(normalized, v)
		}
	}
	return normalized
}

func rowsToMaps(rows *sql.Rows) ([]map[string]any, error) {
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}

	result := make([]map[string]any, 0)
	for rows.Next() {
		values := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}

		row := make(map[string]any, len(cols))
		for i, col := range cols {
			row[col] = values[i]
		}
		result = append(result, row)
	}
	return result, rows.Err()
}

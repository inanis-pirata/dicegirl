package engine

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"dicegirl/internal/apperr"
	"dicegirl/internal/dice"
)

// Random tables are declarative lookup tables: a dice expression picks a
// row, the row's text is the result. They live in the rulebook (game
// content such as the CoC7 insanity tables) and in data packs (content
// that is not part of any game's rules, such as random name lists).
//
// The schema is shared by both:
//
//	tables:
//	  - id: temporary_insanity
//	    name: 临时疯狂症状
//	    roll: 1d10
//	    rows:
//	      - { range: "1", text: "失忆" }
//	      - { range: "2-5", text: "暴力倾向（{1d3} 分钟）" }
//	      - { range: "default", text: "恐惧症" }
//
// A row range is a single value ("3"), an interval ("2-5") or the
// catch-all "default" (the last row; it covers everything the explicit
// ranges leave out). Row texts may embed sub-roll placeholders in braces
// ("{1d3}"): they are rolled when the row is hit and replaced by their
// total.

// rawTable is the YAML shape of one table.
type rawTable struct {
	ID   string        `yaml:"id"`
	Name string        `yaml:"name"`
	Roll string        `yaml:"roll"`
	Rows []rawTableRow `yaml:"rows"`
}

// rawTableRow is one row: a range and the result text.
type rawTableRow struct {
	Range string `yaml:"range"`
	Text  string `yaml:"text"`
}

// TableDef is one compiled random table.
type TableDef struct {
	ID       string     `json:"id"`
	Name     string     `json:"name"`
	RollExpr string     `json:"roll"`
	Min      int        `json:"min"`
	Max      int        `json:"max"`
	Rows     []TableRow `json:"-"`
}

// TableRow is one compiled row. A default row matches every value no
// explicit row claimed.
type TableRow struct {
	Min       int    `json:"min"`
	Max       int    `json:"max"`
	IsDefault bool   `json:"default"`
	Text      string `json:"text"`
	// parts is the parsed row text (literals and sub-roll placeholders).
	parts []tableTextPart
}

// TableResult is the structured outcome of one table roll.
type TableResult struct {
	Kind      string             `json:"kind"`
	TableID   string             `json:"table_id"`
	TableName string             `json:"table_name"`
	Source    string             `json:"source,omitempty"`
	Roll      *dice.RollResult   `json:"roll"`
	Value     int                `json:"value"`
	Row       int                `json:"row"`
	Range     string             `json:"range"`
	Text      string             `json:"text"`
	SubRolls  []*dice.RollResult `json:"subrolls,omitempty"`
}

// TableSet is a compiled collection of tables.
type TableSet struct {
	tables []TableDef
	index  map[string]int
}

// Tables returns the compiled tables in declaration order.
func (ts *TableSet) Tables() []TableDef {
	if ts == nil {
		return nil
	}
	out := make([]TableDef, len(ts.tables))
	copy(out, ts.tables)
	return out
}

// Find returns the table with the given id.
func (ts *TableSet) Find(id string) (TableDef, bool) {
	if ts == nil {
		return TableDef{}, false
	}
	i, ok := ts.index[strings.ToLower(strings.TrimSpace(id))]
	if !ok {
		return TableDef{}, false
	}
	return ts.tables[i], true
}

// Roll rolls the table and returns the hit row with its (sub-)rolls.
func (ts *TableSet) Roll(ctx context.Context, roller Roller, id, source string) (*TableResult, error) {
	const op = "engine.TableSet.Roll"
	t, ok := ts.Find(id)
	if !ok {
		return nil, apperr.New(op, apperr.InvalidArgument, "unknown table: "+id)
	}
	if roller == nil {
		return nil, apperr.New(op, apperr.Internal, "roller not initialized")
	}
	roll, err := roller.Roll(ctx, t.RollExpr)
	if err != nil {
		return nil, err
	}
	value := roll.Total

	idx := -1
	for i, row := range t.Rows {
		if row.IsDefault {
			continue
		}
		if value >= row.Min && value <= row.Max {
			idx = i
			break
		}
	}
	if idx < 0 {
		for i, row := range t.Rows {
			if row.IsDefault {
				idx = i
				break
			}
		}
	}
	if idx < 0 {
		return nil, apperr.New(op, apperr.Internal,
			"table "+t.ID+" has no row for value "+strconv.Itoa(value))
	}
	row := t.Rows[idx]

	text, subRolls, err := renderTableText(ctx, roller, row)
	if err != nil {
		return nil, err
	}
	rangeText := "default"
	if !row.IsDefault {
		rangeText = strconv.Itoa(row.Min)
		if row.Min != row.Max {
			rangeText = strconv.Itoa(row.Min) + "-" + strconv.Itoa(row.Max)
		}
	}
	return &TableResult{
		Kind:      "table_roll",
		TableID:   t.ID,
		TableName: t.Name,
		Source:    source,
		Roll:      roll,
		Value:     value,
		Row:       idx + 1,
		Range:     rangeText,
		Text:      text,
		SubRolls:  subRolls,
	}, nil
}

// compileTables compiles a table list into a TableSet. Every table must
// have a unique id, a parsable roll expression and rows that cover the
// roll's whole domain without overlap.
func compileTables(op string, raw []rawTable) (*TableSet, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	ts := &TableSet{index: map[string]int{}}
	for i, rt := range raw {
		t, err := compileTable(op, i, rt)
		if err != nil {
			return nil, err
		}
		key := strings.ToLower(t.ID)
		if _, dup := ts.index[key]; dup {
			return nil, apperr.New(op, apperr.ConfigInvalid, "duplicate table id "+t.ID)
		}
		ts.index[key] = len(ts.tables)
		ts.tables = append(ts.tables, t)
	}
	return ts, nil
}

// compileTable validates one table declaration.
func compileTable(op string, pos int, raw rawTable) (TableDef, error) {
	where := fmt.Sprintf("table %d", pos)
	if raw.ID == "" || raw.Name == "" {
		return TableDef{}, apperr.New(op, apperr.ConfigInvalid, where+" has an empty id or name")
	}
	where = "table " + raw.ID
	if raw.Roll == "" {
		return TableDef{}, apperr.New(op, apperr.ConfigInvalid, where+" needs a roll expression")
	}
	lo, hi, err := dice.ExprBounds(raw.Roll, dice.DefaultLimits())
	if err != nil {
		return TableDef{}, apperr.Wrap(op, apperr.ConfigInvalid,
			where+" has an invalid roll expression "+raw.Roll, err)
	}
	if lo > hi {
		return TableDef{}, apperr.New(op, apperr.ConfigInvalid,
			where+" has an empty roll domain")
	}
	if len(raw.Rows) == 0 {
		return TableDef{}, apperr.New(op, apperr.ConfigInvalid, where+" has no rows")
	}

	t := TableDef{ID: raw.ID, Name: raw.Name, RollExpr: raw.Roll, Min: lo, Max: hi}
	var explicit []TableRow
	defaultSeen := false
	for i, rr := range raw.Rows {
		rowWhere := fmt.Sprintf("%s row %d", where, i)
		rlo, rhi, isDefault, err := parseTableRange(rr.Range)
		if err != nil {
			return TableDef{}, apperr.Wrap(op, apperr.ConfigInvalid, rowWhere, err)
		}
		parts, err := parseTableText(rr.Text)
		if err != nil {
			return TableDef{}, apperr.Wrap(op, apperr.ConfigInvalid, rowWhere, err)
		}
		if isDefault {
			if defaultSeen {
				return TableDef{}, apperr.New(op, apperr.ConfigInvalid,
					rowWhere+" declares a second default row")
			}
			if i != len(raw.Rows)-1 {
				return TableDef{}, apperr.New(op, apperr.ConfigInvalid,
					rowWhere+" must be the last row")
			}
			defaultSeen = true
			t.Rows = append(t.Rows, TableRow{IsDefault: true, Text: rr.Text, parts: parts})
			continue
		}
		if rlo < lo || rhi > hi {
			return TableDef{}, apperr.New(op, apperr.ConfigInvalid,
				fmt.Sprintf("%s range %d-%d is outside the roll domain %d-%d",
					rowWhere, rlo, rhi, lo, hi))
		}
		row := TableRow{Min: rlo, Max: rhi, Text: rr.Text, parts: parts}
		explicit = append(explicit, row)
		t.Rows = append(t.Rows, row)
	}

	// The explicit rows must not overlap and must tile the whole domain
	// unless a default row covers the remainder.
	sorted := make([]TableRow, len(explicit))
	copy(sorted, explicit)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Min < sorted[j].Min })
	expected := lo
	for _, row := range sorted {
		if row.Min < expected {
			return TableDef{}, apperr.New(op, apperr.ConfigInvalid,
				fmt.Sprintf("%s rows overlap at %d", where, row.Min))
		}
		if row.Min > expected && !defaultSeen {
			return TableDef{}, apperr.New(op, apperr.ConfigInvalid,
				fmt.Sprintf("%s rows leave %d uncovered", where, expected))
		}
		if row.Max >= expected {
			expected = row.Max + 1
		}
	}
	if expected <= hi && !defaultSeen {
		return TableDef{}, apperr.New(op, apperr.ConfigInvalid,
			fmt.Sprintf("%s rows leave %d uncovered", where, expected))
	}
	return t, nil
}

// parseTableRange parses a row range: "3", "2-5" or "default".
func parseTableRange(spec string) (lo, hi int, isDefault bool, err error) {
	s := strings.TrimSpace(spec)
	switch {
	case s == "":
		return 0, 0, false, apperr.New("engine.parseTableRange", apperr.InvalidArgument,
			"empty row range")
	case strings.EqualFold(s, "default"):
		return 0, 0, true, nil
	case strings.Contains(s, "-"):
		parts := strings.SplitN(s, "-", 2)
		a, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
		b, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
		if err1 != nil || err2 != nil || a > b {
			return 0, 0, false, apperr.New("engine.parseTableRange", apperr.InvalidArgument,
				"invalid row range "+spec)
		}
		return a, b, false, nil
	default:
		n, cerr := strconv.Atoi(s)
		if cerr != nil {
			return 0, 0, false, apperr.New("engine.parseTableRange", apperr.InvalidArgument,
				"invalid row range "+spec)
		}
		return n, n, false, nil
	}
}

// tableTextPart is one piece of a row text: a literal chunk or a sub-roll.
type tableTextPart struct {
	literal string
	sub     string // dice expression of a sub-roll placeholder
}

// parseTableText splits a row text into literal chunks and sub-roll
// placeholders ("{1d3}"). The placeholder expressions are validated here so
// a broken table never reaches the roll path.
func parseTableText(text string) ([]tableTextPart, error) {
	var parts []tableTextPart
	var buf strings.Builder
	for i := 0; i < len(text); {
		c := text[i]
		if c != '{' {
			buf.WriteByte(c)
			i++
			continue
		}
		end := strings.IndexByte(text[i:], '}')
		if end < 0 {
			return nil, apperr.New("engine.parseTableText", apperr.InvalidArgument,
				"unclosed '{' in row text")
		}
		end += i
		expr := strings.TrimSpace(text[i+1 : end])
		if expr == "" {
			return nil, apperr.New("engine.parseTableText", apperr.InvalidArgument,
				"empty sub-roll placeholder in row text")
		}
		if _, _, err := dice.Parse(expr, dice.DefaultLimits()); err != nil {
			return nil, apperr.Wrap("engine.parseTableText", apperr.InvalidArgument,
				"invalid sub-roll expression "+expr, err)
		}
		if buf.Len() > 0 {
			parts = append(parts, tableTextPart{literal: buf.String()})
			buf.Reset()
		}
		parts = append(parts, tableTextPart{sub: expr})
		i = end + 1
	}
	if buf.Len() > 0 {
		parts = append(parts, tableTextPart{literal: buf.String()})
	}
	return parts, nil
}

// renderTableText rolls the row's sub-roll placeholders and returns the
// substituted text together with the sub-roll details.
func renderTableText(ctx context.Context, roller Roller, row TableRow) (string, []*dice.RollResult, error) {
	if len(row.parts) == 0 {
		return row.Text, nil, nil
	}
	var sb strings.Builder
	var rolls []*dice.RollResult
	for _, part := range row.parts {
		if part.sub == "" {
			sb.WriteString(part.literal)
			continue
		}
		r, err := roller.Roll(ctx, part.sub)
		if err != nil {
			return "", nil, err
		}
		rolls = append(rolls, r)
		sb.WriteString(strconv.Itoa(r.Total))
	}
	return sb.String(), rolls, nil
}

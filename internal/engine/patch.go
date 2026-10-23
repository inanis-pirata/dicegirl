package engine

// Fine-grained rulebook edits ("house rules") applied as YAML node patches.
//
// A chat command becomes one batch of PatchOps applied in order to the
// rulebook's yaml.Node tree; the result is then handed to ParseBook by the
// application layer before anything is written. ApplyPatch is a pure
// structural transform: it locates nodes and rewrites them, but never
// validates game semantics. Alias clashes, base/base_expr exclusivity,
// level ordering and formula scoping are all left to the regular compile,
// so a bad batch is rejected by ParseBook with zero side effects.
//
// Editing keeps the document readable: untouched nodes keep their comments,
// key order and flow/block style; new Chinese/formula scalars are quoted so
// values like `lowest` or `target/2` can never be misread as YAML 1.1 tags;
// new containers inside a flow mapping are created in flow style.

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"dicegirl/internal/apperr"
)

// PatchKind names one house-rule mutation.
type PatchKind string

// Patch kinds, grouped by rulebook section.
const (
	// Skills: target is the skill id.
	PatchSkillSet      PatchKind = "skill_set" // field base|base_expr|name
	PatchSkillAdd      PatchKind = "skill_add" // value = name of the new skill
	PatchSkillDel      PatchKind = "skill_del"
	PatchSkillAliasAdd PatchKind = "skill_alias_add" // member = alias
	PatchSkillAliasDel PatchKind = "skill_alias_del"
	PatchAttrName      PatchKind = "attr_name" // target = attribute id
	PatchAttrAliasAdd  PatchKind = "attr_alias_add"
	PatchAttrAliasDel  PatchKind = "attr_alias_del"
	PatchLevelName     PatchKind = "level_name" // target = level id
	PatchLevelWhen     PatchKind = "level_when"
	PatchLimitSet      PatchKind = "limit_set"    // target = range, field min|max
	PatchMechanicSet   PatchKind = "mechanic_set" // field names a mechanic scalar
	PatchRulesSet      PatchKind = "rules_set"    // field title|text|keywords
	PatchRulesAdd      PatchKind = "rules_add"    // value = id-less skeleton keyed by target
	PatchRulesDel      PatchKind = "rules_del"
	PatchOpposedTie    PatchKind = "opposed_tie" // value none|higher_target
)

// PatchOp is one fine-grained mutation. Not every field is used by every
// kind:
//
//   - Target: the addressed id or section (skill id, attribute id, level
//     id, rules id, or the limits range name).
//   - Field:  the scalar/field being changed (base, name, when, min, ...).
//   - Value:  the new scalar value (integers arrive as text).
//   - Member: one sequence member (an alias).
//   - Values: a full replacement list (rules keywords).
type PatchOp struct {
	Kind   PatchKind
	Target string
	Field  string
	Value  string
	Member string
	Values []string
}

// ApplyPatch decodes a rulebook YAML document, applies every op in order to
// the node tree and re-encodes it with two-space indentation. A missing
// target is an error and no bytes are returned, so a failed batch leaves no
// partially edited document behind.
func ApplyPatch(data []byte, ops []PatchOp) ([]byte, error) {
	const op = "engine.ApplyPatch"
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, apperr.Wrap(op, apperr.ConfigInvalid, "cannot parse rulebook YAML", err)
	}
	root, err := rootMap(&doc)
	if err != nil {
		return nil, err
	}
	p := &patcher{op: op}
	for i, patch := range ops {
		if err := p.apply(root, patch); err != nil {
			return nil, apperr.Wrap(op, apperr.InvalidArgument,
				"patch #"+strconv.Itoa(i+1)+" ("+string(patch.Kind)+") failed", err)
		}
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return nil, apperr.Wrap(op, apperr.ConfigInvalid, "cannot encode patched rulebook", err)
	}
	if err := enc.Close(); err != nil {
		return nil, apperr.Wrap(op, apperr.ConfigInvalid, "cannot encode patched rulebook", err)
	}
	return buf.Bytes(), nil
}

// ExtractYAML returns the pretty-printed YAML subtree shown by `rule show`.
// section is one of skill/attr/level/rules/limit/mechanic/opposed; id is the
// entry id (ignored for the whole-section forms mechanic/opposed).
func ExtractYAML(data []byte, section, id string) ([]byte, error) {
	const op = "engine.ExtractYAML"
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, apperr.Wrap(op, apperr.ConfigInvalid, "cannot parse rulebook YAML", err)
	}
	root, err := rootMap(&doc)
	if err != nil {
		return nil, err
	}
	node, err := extractNode(op, root, section, id)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(node); err != nil {
		return nil, apperr.Wrap(op, apperr.ConfigInvalid, "cannot encode section", err)
	}
	_ = enc.Close()
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

func extractNode(op string, root *yaml.Node, section, id string) (*yaml.Node, error) {
	switch section {
	case "mechanic":
		return requireMapValue(op, root, "mechanic", "")
	case "opposed":
		return requireMapValue(op, root, "opposed", "")
	case "limit":
		limits := mapValue(root, "limits")
		if limits == nil {
			return nil, apperr.New(op, apperr.InvalidArgument, "rulebook has no limits section")
		}
		return requireMapValue(op, limits, id, "limits range")
	case "skill":
		skills := mapValue(root, "skills")
		if skills == nil {
			return nil, apperr.New(op, apperr.InvalidArgument, "rulebook has no skills section")
		}
		return requireMapValue(op, skills, id, "skill")
	case "attr":
		attrs := mapValue(root, "attributes")
		if attrs == nil {
			return nil, apperr.New(op, apperr.InvalidArgument, "rulebook has no attributes section")
		}
		return seqEntryByID(op, attrs, id, "attribute")
	case "level":
		checks := mapValue(root, "checks")
		if checks == nil {
			return nil, apperr.New(op, apperr.InvalidArgument, "rulebook has no checks section")
		}
		levels := mapValue(checks, "levels")
		if levels == nil {
			return nil, apperr.New(op, apperr.InvalidArgument, "rulebook declares no levels")
		}
		return seqEntryByID(op, levels, id, "level")
	case "rules":
		rules := mapValue(root, "rules")
		if rules == nil {
			return nil, apperr.New(op, apperr.InvalidArgument, "rulebook declares no quick-reference rules")
		}
		return seqEntryByID(op, rules, id, "rule entry")
	default:
		return nil, apperr.New(op, apperr.InvalidArgument, "unknown show section "+section)
	}
}

// patcher carries the error op name through the mutations.
type patcher struct{ op string }

func (p *patcher) failf(format string, args ...any) error {
	return apperr.New(p.op, apperr.InvalidArgument, fmt.Sprintf(format, args...))
}

func (p *patcher) apply(root *yaml.Node, o PatchOp) error {
	switch o.Kind {
	case PatchSkillSet:
		skill, err := p.skill(root, o.Target)
		if err != nil {
			return err
		}
		return p.setSkillField(skill, o.Field, o.Value)
	case PatchSkillAdd:
		return p.skillAdd(root, o.Target, o.Value)
	case PatchSkillDel:
		skills, err := p.section(root, "skills")
		if err != nil {
			return err
		}
		return removeMapKey(skills, o.Target, "skill")
	case PatchSkillAliasAdd, PatchSkillAliasDel:
		skill, err := p.skill(root, o.Target)
		if err != nil {
			return err
		}
		return editAlias(skill, o.Member, o.Kind == PatchSkillAliasAdd)
	case PatchAttrName:
		attr, err := p.attr(root, o.Target)
		if err != nil {
			return err
		}
		return setScalarField(attr, "name", strScalar(o.Value))
	case PatchAttrAliasAdd, PatchAttrAliasDel:
		attr, err := p.attr(root, o.Target)
		if err != nil {
			return err
		}
		return editAlias(attr, o.Member, o.Kind == PatchAttrAliasAdd)
	case PatchLevelName:
		level, err := p.level(root, o.Target)
		if err != nil {
			return err
		}
		return setScalarField(level, "name", strScalar(o.Value))
	case PatchLevelWhen:
		level, err := p.level(root, o.Target)
		if err != nil {
			return err
		}
		return setScalarField(level, "when", strScalar(o.Value))
	case PatchLimitSet:
		return p.setLimit(root, o.Target, o.Field, o.Value)
	case PatchMechanicSet:
		mech, err := p.section(root, "mechanic")
		if err != nil {
			return err
		}
		return p.setMechanicField(mech, o.Field, o.Value)
	case PatchRulesSet:
		entry, err := p.rule(root, o.Target)
		if err != nil {
			return err
		}
		return p.setRulesField(entry, o)
	case PatchRulesAdd:
		return p.ruleAdd(root, o.Target)
	case PatchRulesDel:
		rules, err := p.section(root, "rules")
		if err != nil {
			return err
		}
		return removeSeqEntry(rules, o.Target, "rule entry")
	case PatchOpposedTie:
		opposed, err := p.section(root, "opposed")
		if err != nil {
			return err
		}
		return setScalarField(opposed, "tie_break", strScalar(o.Value))
	default:
		return p.failf("unknown patch kind %s", o.Kind)
	}
}

func (p *patcher) section(root *yaml.Node, key string) (*yaml.Node, error) {
	n := mapValue(root, key)
	if n == nil {
		return nil, p.failf("rulebook has no %s section", key)
	}
	return n, nil
}

func (p *patcher) skill(root *yaml.Node, id string) (*yaml.Node, error) {
	skills, err := p.section(root, "skills")
	if err != nil {
		return nil, err
	}
	skill := mapValue(skills, id)
	if skill == nil {
		return nil, p.failf("unknown skill %s", id)
	}
	return skill, nil
}

func (p *patcher) attr(root *yaml.Node, id string) (*yaml.Node, error) {
	attrs, err := p.section(root, "attributes")
	if err != nil {
		return nil, err
	}
	entry := seqFindByID(attrs, id)
	if entry == nil {
		return nil, p.failf("unknown attribute %s", id)
	}
	return entry, nil
}

func (p *patcher) level(root *yaml.Node, id string) (*yaml.Node, error) {
	checks, err := p.section(root, "checks")
	if err != nil {
		return nil, err
	}
	levels := mapValue(checks, "levels")
	if levels == nil {
		return nil, p.failf("rulebook declares no levels")
	}
	entry := seqFindByID(levels, id)
	if entry == nil {
		return nil, p.failf("unknown level %s", id)
	}
	return entry, nil
}

func (p *patcher) rule(root *yaml.Node, id string) (*yaml.Node, error) {
	rules, err := p.section(root, "rules")
	if err != nil {
		return nil, err
	}
	entry := seqFindByID(rules, id)
	if entry == nil {
		return nil, p.failf("unknown rule entry %s", id)
	}
	return entry, nil
}

// setSkillField rewrites one skill scalar. base and base_expr are mutually
// exclusive in the schema, so setting one drops the other.
func (p *patcher) setSkillField(skill *yaml.Node, field, value string) error {
	switch field {
	case "base":
		n, err := parseIntScalar(p.op, value)
		if err != nil {
			return err
		}
		removeMapKey(skill, "base_expr", "")
		return setScalarField(skill, "base", n)
	case "base_expr":
		removeMapKey(skill, "base", "")
		return setScalarField(skill, "base_expr", strScalar(value))
	case "name":
		return setScalarField(skill, "name", strScalar(value))
	default:
		return p.failf("skill field must be base, base_expr or name, got %s", field)
	}
}

func (p *patcher) skillAdd(root *yaml.Node, id, name string) error {
	if id == "" || name == "" {
		return p.failf("skill_add needs an id and a name")
	}
	skills := mapValue(root, "skills")
	if skills == nil {
		return p.failf("rulebook has no skills section")
	}
	if mapValue(skills, id) != nil {
		return p.failf("skill %s already exists", id)
	}
	entry := &yaml.Node{Kind: yaml.MappingNode, Style: yaml.FlowStyle}
	entry.Content = append(entry.Content,
		plainKey("name"), strScalar(name))
	skills.Content = append(skills.Content, plainKey(id), entry)
	return nil
}

func (p *patcher) setLimit(root *yaml.Node, seg, field, value string) error {
	limits, err := p.section(root, "limits")
	if err != nil {
		return err
	}
	rng := mapValue(limits, seg)
	if rng == nil {
		if seg != "computed_target" {
			return p.failf("unknown limits range %s", seg)
		}
		// computed_target is the only optional range; create it in flow
		// style to match its neighbors.
		rng = &yaml.Node{Kind: yaml.MappingNode, Style: yaml.FlowStyle}
		setMapValue(limits, seg, rng)
	}
	n, err := parseIntScalar(p.op, value)
	if err != nil {
		return err
	}
	switch field {
	case "min", "max":
		return setScalarField(rng, field, n)
	default:
		return p.failf("limit field must be min or max, got %s", field)
	}
}

func (p *patcher) setMechanicField(mech *yaml.Node, field, value string) error {
	switch field {
	case "roll", "meets", "label", "bonus_keep", "penalty_keep":
		return setScalarField(mech, field, strScalar(value))
	case "max_bonus_penalty":
		n, err := parseIntScalar(p.op, value)
		if err != nil {
			return err
		}
		return setScalarField(mech, field, n)
	default:
		return p.failf("unknown mechanic field %s", field)
	}
}

func (p *patcher) setRulesField(entry *yaml.Node, o PatchOp) error {
	switch o.Field {
	case "title", "text":
		return setScalarField(entry, o.Field, strScalar(o.Value))
	case "keywords":
		if len(o.Values) == 0 {
			removeMapKey(entry, "keywords", "")
			return nil
		}
		seq := &yaml.Node{Kind: yaml.SequenceNode, Style: yaml.FlowStyle}
		for _, k := range o.Values {
			seq.Content = append(seq.Content, strScalar(k))
		}
		return setScalarField(entry, "keywords", seq)
	default:
		return p.failf("rules field must be title, text or keywords, got %s", o.Field)
	}
}

func (p *patcher) ruleAdd(root *yaml.Node, id string) error {
	if id == "" {
		return p.failf("rules_add needs an id")
	}
	rules := mapValue(root, "rules")
	if rules == nil {
		rules = &yaml.Node{Kind: yaml.SequenceNode}
		setMapValue(root, "rules", rules)
	}
	if rules.Kind != yaml.SequenceNode {
		return p.failf("rules section is not a sequence")
	}
	if seqFindByID(rules, id) != nil {
		return p.failf("rule entry %s already exists", id)
	}
	entry := &yaml.Node{Kind: yaml.MappingNode}
	entry.Content = append(entry.Content, plainKey("id"), &yaml.Node{
		Kind: yaml.ScalarNode, Tag: "!!str", Value: id,
	})
	rules.Content = append(rules.Content, entry)
	return nil
}

// --- generic node helpers --------------------------------------------------

func rootMap(doc *yaml.Node) (*yaml.Node, error) {
	const op = "engine.ApplyPatch"
	if doc.Kind != yaml.DocumentNode || len(doc.Content) != 1 {
		return nil, apperr.New(op, apperr.ConfigInvalid, "rulebook YAML has no document node")
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil, apperr.New(op, apperr.ConfigInvalid, "rulebook root must be a mapping")
	}
	return root, nil
}

// mapValue returns the value node paired with key in a mapping, or nil.
func mapValue(m *yaml.Node, key string) *yaml.Node {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

// setMapValue replaces the value of key or appends a new pair.
func setMapValue(m *yaml.Node, key string, val *yaml.Node) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			preserveComment(m.Content[i+1], val)
			m.Content[i+1] = val
			return
		}
	}
	m.Content = append(m.Content, plainKey(key), val)
}

// setScalarField sets a mapping field, routing through setMapValue so a
// missing field is appended rather than dropped.
func setScalarField(m *yaml.Node, key string, val *yaml.Node) error {
	if m == nil || m.Kind != yaml.MappingNode {
		return apperr.New("engine.ApplyPatch", apperr.InvalidArgument, "expected a mapping node")
	}
	setMapValue(m, key, val)
	return nil
}

// removeMapKey deletes the key/value pair, reporting whether it existed. The
// label is only used for the not-found error; an empty label makes a missing
// key a no-op (used when clearing mutually exclusive fields).
func removeMapKey(m *yaml.Node, key, label string) error {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			m.Content = append(m.Content[:i], m.Content[i+2:]...)
			return nil
		}
	}
	if label != "" {
		return apperr.New("engine.ApplyPatch", apperr.InvalidArgument, "unknown "+label+" "+key)
	}
	return nil
}

// seqFindByID returns the mapping element whose `id` scalar equals id.
func seqFindByID(seq *yaml.Node, id string) *yaml.Node {
	if seq == nil || seq.Kind != yaml.SequenceNode {
		return nil
	}
	for _, el := range seq.Content {
		if el.Kind == yaml.MappingNode && mapValue(el, "id") != nil && mapValue(el, "id").Value == id {
			return el
		}
	}
	return nil
}

// seqEntryByID is the error-returning form used by ExtractYAML.
func seqEntryByID(op string, seq *yaml.Node, id, label string) (*yaml.Node, error) {
	if el := seqFindByID(seq, id); el != nil {
		return el, nil
	}
	return nil, apperr.New(op, apperr.InvalidArgument, "unknown "+label+" "+id)
}

// removeSeqEntry deletes the mapping element with the given id.
func removeSeqEntry(seq *yaml.Node, id, label string) error {
	if seq.Kind != yaml.SequenceNode {
		return apperr.New("engine.ApplyPatch", apperr.InvalidArgument, label+" section is not a sequence")
	}
	for i, el := range seq.Content {
		if el.Kind == yaml.MappingNode && mapValue(el, "id") != nil && mapValue(el, "id").Value == id {
			seq.Content = append(seq.Content[:i], seq.Content[i+1:]...)
			return nil
		}
	}
	return apperr.New("engine.ApplyPatch", apperr.InvalidArgument, "unknown "+label+" "+id)
}

// requireMapValue fetches a mapping value with a typed not-found error.
func requireMapValue(op string, m *yaml.Node, key, label string) (*yaml.Node, error) {
	if v := mapValue(m, key); v != nil {
		return v, nil
	}
	if label == "" {
		return nil, apperr.New(op, apperr.InvalidArgument, "section "+key+" is missing")
	}
	return nil, apperr.New(op, apperr.InvalidArgument, "unknown "+label+" "+key)
}

// editAlias adds or removes one member of a mapping's flow `aliases`
// sequence, creating/deleting the field as needed.
func editAlias(m *yaml.Node, alias string, add bool) error {
	const op = "engine.ApplyPatch"
	alias = strings.TrimSpace(alias)
	if alias == "" {
		return apperr.New(op, apperr.InvalidArgument, "alias must not be empty")
	}
	seq := mapValue(m, "aliases")
	if add {
		if seq == nil {
			seq = &yaml.Node{Kind: yaml.SequenceNode, Style: yaml.FlowStyle}
			setMapValue(m, "aliases", seq)
		}
		if seq.Kind != yaml.SequenceNode {
			return apperr.New(op, apperr.InvalidArgument, "aliases is not a sequence")
		}
		for _, el := range seq.Content {
			if el.Value == alias {
				return apperr.New(op, apperr.InvalidArgument, "alias already present: "+alias)
			}
		}
		seq.Content = append(seq.Content, strScalar(alias))
		return nil
	}
	if seq == nil {
		return apperr.New(op, apperr.InvalidArgument, "alias not present: "+alias)
	}
	kept := seq.Content[:0]
	found := false
	for _, el := range seq.Content {
		if el.Value == alias {
			found = true
			continue
		}
		kept = append(kept, el)
	}
	if !found {
		return apperr.New(op, apperr.InvalidArgument, "alias not present: "+alias)
	}
	if len(kept) == 0 {
		return removeMapKey(m, "aliases", "")
	}
	seq.Content = kept
	return nil
}

func plainKey(s string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: s}
}

// strScalar builds a double-quoted string scalar so values such as `lowest`
// or `target/2` keep their string type under YAML 1.1 rules.
func strScalar(s string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: s, Style: yaml.DoubleQuotedStyle}
}

func parseIntScalar(op, raw string) (*yaml.Node, error) {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return nil, apperr.Wrap(op, apperr.InvalidArgument, "expected an integer, got "+raw, err)
	}
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.Itoa(n)}, nil
}

// preserveComment carries the trailing comment of a replaced value onto its
// replacement so scalar edits don't strip the line's annotation.
func preserveComment(old, fresh *yaml.Node) {
	if old == nil || fresh == nil {
		return
	}
	if fresh.LineComment == "" {
		fresh.LineComment = old.LineComment
	}
}

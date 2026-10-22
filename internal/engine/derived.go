package engine

import (
	"dicegirl/internal/apperr"
	"dicegirl/internal/character"
	"dicegirl/internal/engine/formula"
)

// DerivedValue is one computed statistic. Either Int or Expr is set
// (lookup of a bonus expression yields Expr).
type DerivedValue struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Int  int    `json:"int"`
	Expr string `json:"expr,omitempty"`
}

// VitalsValue is a resource track's proposed initial/maximum values.
type VitalsValue struct {
	Resource string `json:"resource"`
	Current  int    `json:"current"`
	Max      int    `json:"max"`
}

// InitAttribute is a proposed initial attribute assignment.
type InitAttribute struct {
	Attribute string `json:"attribute"`
	Value     int    `json:"value"`
}

// DerivedResult holds everything the rulebook derives from a sheet.
type DerivedResult struct {
	Kind           string          `json:"kind"`
	SystemID       string          `json:"system_id"`
	Values         []DerivedValue  `json:"values"`
	Vitals         []VitalsValue   `json:"vitals"`
	InitAttributes []InitAttribute `json:"init_attributes"`

	ints map[string]int
}

// Int returns an integer derived value by id.
func (d *DerivedResult) Int(id string) (int, bool) {
	v, ok := d.ints[id]
	return v, ok
}

// Derive computes every declared derived statistic.
func (b *Book) Derive(c *character.Character) (*DerivedResult, error) {
	const op = "engine.Derive"
	if c == nil {
		return nil, apperr.New(op, apperr.InvalidArgument, "nil character")
	}
	env := formula.Env{}
	for k, v := range c.Attributes {
		env[k] = v
	}
	res := &DerivedResult{Kind: "derived", SystemID: b.manifest.ID, ints: map[string]int{}}

	for _, dv := range b.derived {
		switch {
		case dv.formula != nil:
			v, err := dv.formula.EvalInt(env)
			if err != nil {
				return nil, apperr.Wrap(op, apperr.InvalidArgument,
					"cannot derive "+dv.id, err)
			}
			res.Values = append(res.Values, DerivedValue{ID: dv.id, Name: dv.name, Int: v})
			env[dv.id] = v
			res.ints[dv.id] = v
		case len(dv.cases) > 0:
			v, err := evalSwitch(dv, env)
			if err != nil {
				return nil, apperr.Wrap(op, apperr.InvalidArgument,
					"cannot derive "+dv.id, err)
			}
			res.Values = append(res.Values, DerivedValue{ID: dv.id, Name: dv.name, Int: v})
			env[dv.id] = v
			res.ints[dv.id] = v
		case dv.lookup != "":
			key, err := b.dbTable.key.EvalInt(env)
			if err != nil {
				return nil, apperr.Wrap(op, apperr.InvalidArgument,
					"cannot compute damage-bonus key for "+dv.id, err)
			}
			row, err := b.LookupDamageBonus(key)
			if err != nil {
				return nil, err
			}
			if dv.field == "bonus" {
				res.Values = append(res.Values, DerivedValue{ID: dv.id, Name: dv.name, Expr: row.Bonus})
			} else {
				res.Values = append(res.Values, DerivedValue{ID: dv.id, Name: dv.name, Int: row.Build})
				env[dv.id] = row.Build
				res.ints[dv.id] = row.Build
			}
		}
	}

	for _, vd := range b.vitals {
		max, err := vd.max.EvalInt(env)
		if err != nil {
			return nil, apperr.Wrap(op, apperr.InvalidArgument,
				"cannot derive max of resource "+vd.resource, err)
		}
		cur, err := vd.initial.EvalInt(env)
		if err != nil {
			return nil, apperr.Wrap(op, apperr.InvalidArgument,
				"cannot derive initial value of resource "+vd.resource, err)
		}
		res.Vitals = append(res.Vitals, VitalsValue{Resource: vd.resource, Current: cur, Max: max})
	}

	for _, ia := range b.initAttrs {
		v, err := ia.value.EvalInt(env)
		if err != nil {
			return nil, apperr.Wrap(op, apperr.InvalidArgument,
				"cannot derive initial attribute "+ia.attribute, err)
		}
		res.InitAttributes = append(res.InitAttributes, InitAttribute{Attribute: ia.attribute, Value: v})
	}
	return res, nil
}

// ResourceMax returns the declared maximum of a resource track using the
// character's attributes and derived values.
func (b *Book) ResourceMax(c *character.Character, resource string) (int, bool, error) {
	d, err := b.Derive(c)
	if err != nil {
		return 0, false, err
	}
	for _, v := range d.Vitals {
		if v.Resource == resource {
			return v.Max, true, nil
		}
	}
	return 0, false, nil
}

func evalSwitch(dv compiledDerived, env formula.Env) (int, error) {
	for _, c := range dv.cases {
		if c.isDefault {
			return c.value, nil
		}
		ok, err := c.when.EvalBool(env)
		if err != nil {
			return 0, err
		}
		if ok {
			return c.value, nil
		}
	}
	return 0, apperr.New("engine.evalSwitch", apperr.Internal,
		"no switch case matched for "+dv.id+" (default case missing)")
}

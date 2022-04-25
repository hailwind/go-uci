package uci

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestUnmangleSectionName(t *testing.T) {
	tt := map[string]struct {
		typ string
		idx int
		err string
	}{
		// simple test cases
		"":            {err: "implausible section selector: must be at least 5 characters long"},
		"aa[0]":       {err: "invalid syntax: section selector must start with @ sign"},
		"@@[0]":       {err: "invalid syntax: multiple @ signs found"},
		"@@@@@@@@@@@": {err: "invalid syntax: multiple @ signs found"},
		"@[[0]":       {err: "invalid syntax: multiple open brackets found"},
		"@][0]":       {err: "invalid syntax: multiple closed brackets found"},
		"@aa0]":       {err: "invalid syntax: section selector must have format '@type[index]'"},
		"@a[b]":       {err: `invalid syntax: index must be numeric: strconv.Atoi: parsing "b": invalid syntax`},

		// valid test cases
		"@a[0]":    {typ: "a", idx: 0},
		"@a[4223]": {typ: "a", idx: 4223},
		"@a[-1]":   {typ: "a", idx: -1},

		// longer types/indices
		"@abcdEFGHijkl[-255]": {typ: "abcdEFGHijkl", idx: -255},
		"@abcdEFGHijkl[0xff]": {err: `invalid syntax: index must be numeric: strconv.Atoi: parsing "0xff": invalid syntax`},
	}

	for input := range tt {
		input, tc := input, tt[input]
		t.Run(input, func(t *testing.T) {
			assert := assert.New(t)
			typ, idx, err := unmangleSectionName(input)

			if tc.err != "" {
				assert.EqualError(err, tc.err)
			} else {
				assert.NoError(err)
				assert.Equal(tc.idx, idx)
				assert.Equal(tc.typ, typ)
			}
		})
	}
}

func TestConfigGet(t *testing.T) { //nolint:funlen
	config, err := parse("unnamed", tcUnnamedInput)
	assert.NoError(t, err)

	cases := []*Section{
		// for fun, tcUnnamedInput starts with a named section. for extra
		// fun, tcUnnamedInput extends the named section at the end.
		{"named", "foo", []*Option{
			NewOption("pos", TypeOption, "3"), // gets overwritten by last section
			NewOption("unnamed", TypeOption, "0"),
			NewOption("list", TypeList, "0", "30"), // gets merged with last Section
		}},

		// the @foo[0] selector only compares type (foo) and index (0)
		{"@foo[0]", "foo", []*Option{ // alias for "named"
			NewOption("pos", TypeOption, "3"),
			NewOption("unnamed", TypeOption, "0"),
			NewOption("list", TypeList, "0", "30"),
		}},
		{"@foo[1]", "foo", []*Option{
			NewOption("pos", TypeOption, "1"),
			NewOption("unnamed", TypeOption, "1"),
			NewOption("list", TypeOption, "10"),
		}},
		{"@foo[2]", "foo", []*Option{
			NewOption("pos", TypeOption, "2"),
			NewOption("unnamed", TypeOption, "1"),
			NewOption("list", TypeList, "20"),
		}},

		// negative indices count from the end
		{"@foo[-3]", "foo", []*Option{ // alias for "@foo[0]" == "named"
			NewOption("pos", TypeOption, "3"),
			NewOption("unnamed", TypeOption, "0"),
			NewOption("list", TypeList, "0", "30"),
		}},
		{"@foo[-2]", "foo", []*Option{ // alias for "@foo[1]"
			NewOption("pos", TypeOption, "1"),
			NewOption("unnamed", TypeOption, "1"),
			NewOption("list", TypeList, "10"),
		}},
		{"@foo[-1]", "foo", []*Option{ // alias for "@foo[2]"
			NewOption("pos", TypeOption, "2"),
			NewOption("unnamed", TypeOption, "1"),
			NewOption("list", TypeList, "20"),
		}},
	}

	for i := range cases {
		s := cases[i]
		for j := range s.Options {
			o := s.Options[j]
			t.Run("unnamed."+s.Name+"."+o.Name, func(t *testing.T) {
				sec := config.Get(s.Name)
				if !assert.NotNil(t, sec) {
					return
				}

				opt := sec.Get(o.Name)
				if !assert.NotNil(t, opt) {
					return
				}

				assert.EqualValues(t, o.Values, opt.Values)
			})
		}
	}
}

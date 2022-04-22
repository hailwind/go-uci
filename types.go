package uci

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
)

// NOTE: config, section and option types basically are AST nodes for the
// parser. The JSON struct tags are mainly for development and testing
// purposes: We'er generating JSON dumps of the tree when running tests
// with DUMP="json". After a manual comparison with the corresponding UCI
// file in testdata/, we can use the dumps to read them back as test case
// expectations.

// Config represents a file in UCI. It consists of sections.
type Config struct {
	Name     string     `json:"name"`
	Sections []*Section `json:"sections,omitempty"`

	tainted bool // changed by tree methods when things were modified
}

// newConfig returns a new config object.
func newConfig(name string) *Config {
	return &Config{
		Name:     name,
		Sections: make([]*Section, 0, 1),
	}
}

func (c *Config) WriteTo(w io.Writer) (n int64, err error) {
	var buf bytes.Buffer

	for _, sec := range c.Sections {
		if sec.Name == "" || IsPlaceholderName(sec.Name, sec.Type) {
			_, _ = fmt.Fprintf(&buf, "\nconfig %s\n", sec.Type)
		} else {
			_, _ = fmt.Fprintf(&buf, "\nconfig %s '%s'\n", sec.Type, sec.Name)
		}

		for _, opt := range sec.Options {
			switch opt.Type {
			case TypeOption:
				_, _ = fmt.Fprintf(&buf, "\toption %s '%s'\n", opt.Name, opt.Values[0])
			case TypeList:
				for _, v := range opt.Values {
					_, _ = fmt.Fprintf(&buf, "\tlist %s '%s'\n", opt.Name, v)
				}
			}
		}
	}
	buf.WriteByte('\n')
	return buf.WriteTo(w)
}

// Get fetches a section by name.
//
// Support for unnamed Section notation (@foo[idx]) is present.
func (c *Config) Get(name string) *Section {
	if strings.HasPrefix(name, "@") {
		sec, _ := c.getUnnamed(name) // TODO: log error?
		return sec
	}
	return c.getNamed(name)
}

func (c *Config) getNamed(name string) *Section {
	for _, sec := range c.Sections {
		if sec.Name == name {
			return sec
		}
	}
	return nil
}

var (
	ErrImplausibleSectionSelector = errors.New("implausible section selector: must be at least 5 characters long")
	ErrMustStartWithAt            = errors.New("invalid syntax: section selector must start with @ sign")
	ErrMultipleAtSigns            = errors.New("invalid syntax: multiple @ signs found")
	ErrMultipleOpenBrackets       = errors.New("invalid syntax: multiple open brackets found")
	ErrMultipleCloseBrackets      = errors.New("invalid syntax: multiple closed brackets found")
	ErrInvalidSectionSelector     = errors.New("invalid syntax: section selector must have format '@type[index]'")
)

func unmangleSectionName(name string) (typ string, index int, err error) { //nolint:cyclop
	l := len(name)
	if l < 5 { // "@a[0]"
		err = ErrImplausibleSectionSelector
		return
	}
	if name[0] != '@' {
		err = ErrMustStartWithAt
		return
	}

	bra, ket := 0, l-1 // bracket positions
	for i, r := range name {
		switch {
		case i != 0 && r == '@':
			err = ErrMultipleAtSigns
			return
		case r == '[' && bra > 0:
			err = ErrMultipleOpenBrackets
			return
		case r == ']' && i != ket:
			err = ErrMultipleCloseBrackets
			return
		case r == '[':
			bra = i
		}
	}

	if bra == 0 || bra >= ket {
		err = ErrInvalidSectionSelector
		return
	}

	typ = name[1:bra]
	index, err = strconv.Atoi(name[bra+1 : ket])
	if err != nil {
		err = fmt.Errorf("invalid syntax: index must be numeric: %w", err)
	}
	return typ, index, err
}

var ErrUnnamedIndexOutOfBounds = errors.New("invalid name: index out of bounds")

func (c *Config) getUnnamed(name string) (*Section, error) {
	typ, idx, err := unmangleSectionName(name)
	if err != nil {
		return nil, err
	}

	count := c.count(typ)
	if -count > idx || idx >= count {
		return nil, ErrUnnamedIndexOutOfBounds
	}
	if idx < 0 {
		idx += count // count from the end
	}

	for i, n := 0, 0; i < len(c.Sections); i++ {
		if c.Sections[i].Type == typ {
			if idx == n {
				return c.Sections[i], nil
			}
			n++
		}
	}
	return nil, nil
}

func (c *Config) Add(s *Section) *Section {
	c.Sections = append(c.Sections, s)
	return s
}

func (c *Config) Merge(s *Section) *Section {
	var sec *Section
	for i := range c.Sections {
		sname := c.sectionName(s)
		cname := c.sectionName(c.Sections[i])

		if sname == cname {
			sec = c.Sections[i]
			break
		}
	}

	if sec == nil {
		return c.Add(s)
	}
	for _, o := range s.Options {
		sec.Merge(o)
	}
	return sec
}

func (c *Config) Del(name string) {
	var i int
	indexs := make(map[string]int, 5)
	for i = 0; i < len(c.Sections); i++ {
		if IsPlaceholderName(name, c.Sections[i].Type) {
			_, index, _ := unmangleSectionName(name)

			if _, ok := indexs[c.Sections[i].Type]; !ok {
				indexs[c.Sections[i].Type] = 0
			}

			if index == indexs[c.Sections[i].Type] {
				break
			}

			indexs[c.Sections[i].Type]++
		}

		if c.Sections[i].Name == name {
			break
		}
	}
	if i < len(c.Sections) {
		c.Sections = append(c.Sections[:i], c.Sections[i+1:]...)
	}
}

func (c *Config) sectionName(s *Section) string {
	if s.Name != "" {
		return s.Name
	}
	return fmt.Sprintf("@%s[%d]", s.Type, c.index(s))
}

func (c *Config) index(s *Section) (i int) {
	for _, sec := range c.Sections {
		if sec == s {
			return i
		}
		if sec.Type == s.Type {
			i++
		}
	}
	panic("not reached")
}

func (c *Config) count(typ string) (n int) {
	for _, sec := range c.Sections {
		if sec.Type == typ {
			n++
		}
	}
	return
}

// A Section represents a group of options in UCI. It may be named or
// unnamed. In the latter case, its synthetic name is constructed from
// the Section type and index (e.g. "@system[0]").
type Section struct {
	Name    string    `json:"name,omitempty"`
	Type    string    `json:"type"`
	Options []*Option `json:"options,omitempty"`
}

// newSection returns a new Section object.
func newSection(typ, name string) *Section {
	return &Section{
		Type:    typ,
		Name:    name,
		Options: make([]*Option, 0, 1),
	}
}

func (s *Section) Add(o *Option) {
	s.Options = append(s.Options, o)
}

func (s *Section) Merge(o *Option) {
	for _, opt := range s.Options {
		if opt.Name == o.Name {
			opt.MergeValues(o.Values...)
			return
		}
	}
	s.Options = append(s.Options, o)
}

// Del removes an Option with the given name. It returns whether the
// Option actually existed.
func (s *Section) Del(name string) bool {
	var i int
	for i = 0; i < len(s.Options); i++ {
		if s.Options[i].Name == name {
			break
		}
	}

	if i == len(s.Options) {
		return false
	}

	s.Options = append(s.Options[:i], s.Options[i+1:]...)

	return true
}

// Get fetches an Option by name.
func (s *Section) Get(name string) *Option {
	for _, opt := range s.Options {
		if opt.Name == name {
			return opt
		}
	}
	return nil
}

func (s *Section) OptionValue(name string, values ...string) []string {
	for _, opt := range s.Options {
		if opt.Name == name {
			return opt.Values
		}
	}
	return values
}

func (s *Section) OptionLastValue(name string, value string) string {
	for _, opt := range s.Options {
		if opt.Name == name {
			return opt.Values[len(opt.Values)-1]
		}
	}
	return value
}

// An Option is the key to one or more values. Multiple values indicate
// a list option.
type Option struct {
	Name   string     `json:"name"`
	Values []string   `json:"values"`
	Type   OptionType `json:"type"`
}

// newOption returns a new option object.
func newOption(name string, optionType OptionType, values ...string) *Option {
	return &Option{
		Name:   name,
		Values: values,
		Type:   optionType,
	}
}

func (o *Option) SetValues(vs ...string) {
	o.Values = vs
}

func (o *Option) AddValue(v string) {
	o.Values = append(o.Values, v)
}

func (o *Option) MergeValues(vs ...string) {
	have := make(map[string]struct{})
	for _, v := range o.Values {
		have[v] = struct{}{}
	}

	for _, v := range vs {
		if _, exists := have[v]; exists {
			continue
		}
		o.AddValue(v)
	}
}

var placeholderSectionPattern, _ = regexp.Compile(`^@(.*?)\[(\d+)\]$`)

func Num2PlaceholderSection(sectionType string, num int) string {
	return strings.Join([]string{"@", sectionType, "[", strconv.Itoa(num), "]"}, "")
}

func PlaceholderSection2Num(section string) (int, error) {
	submatchs := placeholderSectionPattern.FindStringSubmatch(section)

	if len(submatchs) < 3 {
		return 0, errors.New("匿名section 格式错误")
	}

	atoi, _ := strconv.Atoi(submatchs[2])

	return atoi, nil
}

func IsPlaceholderName(name, secType string) bool {
	expr := strings.Join([]string{"^@", secType, `\[(\d+)\]$`}, "")
	placeholderNameRegexp := regexp.MustCompile(expr)

	return placeholderNameRegexp.MatchString(name)
}

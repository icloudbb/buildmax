package conversation

import "testing"

func TestInputSchemaOf(t *testing.T) {
	cases := []struct {
		name       string
		definition string
		want       string
	}{
		{
			name:       "declares input_schema",
			definition: `{"schema_version":1,"input_schema":{"type":"object"},"nodes":[]}`,
			want:       `{"type":"object"}`,
		},
		{
			name:       "no input_schema",
			definition: `{"schema_version":1,"nodes":[]}`,
			want:       "",
		},
		{
			name:       "unparseable definition",
			definition: `not json`,
			want:       "",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := inputSchemaOf(c.definition); got != c.want {
				t.Errorf("inputSchemaOf = %q, want %q", got, c.want)
			}
		})
	}
}

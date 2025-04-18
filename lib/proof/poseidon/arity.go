package poseidondst

type Arity interface {
	Arity() int
}

type Arity2 struct{}
type Arity3 struct{}
type Arity4 struct{}
type Arity8 struct{}
type Arity16 struct{}
type Arity32 struct{}
type Arity24 struct{}
type Arity36 struct{}
type Arity11 struct{}

func (a Arity2) Arity() int  { return 2 }
func (a Arity3) Arity() int  { return 3 }
func (a Arity4) Arity() int  { return 4 }
func (a Arity8) Arity() int  { return 8 }
func (a Arity16) Arity() int { return 16 }
func (a Arity32) Arity() int { return 32 }
func (a Arity24) Arity() int { return 24 }
func (a Arity36) Arity() int { return 36 }
func (a Arity11) Arity() int { return 11 }

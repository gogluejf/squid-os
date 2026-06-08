package app

type AuthResult struct {
	Approved     bool
	Instructions string // empty = plain yes/no
}

func (r AuthResult) HasInstructions() bool { return r.Instructions != "" }

type AuthorizationContext struct {
	ToolName      string
	Args          map[string]interface{}
	ArgsJSON      string
	DisplayValue  string
	IsDestructive bool
	Result        AuthResult
}

func (c *AuthorizationContext) IsActionable() bool {
	return c.Result.Approved || c.Result.Instructions != ""
}

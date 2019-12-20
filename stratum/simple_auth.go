package stratum

type SimpleAuth struct {
	passwd string
}

func NewSimpleAuth(passwd string) *SimpleAuth {
	return &SimpleAuth{
		passwd: passwd,
	}
}

func (this *SimpleAuth) Auth(username string, passwd string) bool {
	if this.passwd == "" || this.passwd == passwd {
		return true
	} else {
		return false
	}
}

type Auth interface {
	Auth(username string, passwd string) bool
}

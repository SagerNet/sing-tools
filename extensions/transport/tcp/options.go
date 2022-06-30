package tcp

import "github.com/sagernet/sing-tools/extensions/redir"

type Option func(*Listener)

func WithTransproxyMode(mode redir.TransproxyMode) Option {
	return func(listener *Listener) {
		listener.trans = mode
	}
}

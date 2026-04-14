package handlers

import (
	"rssh/internal/server/users"
	"rssh/pkg/logger"
	"golang.org/x/crypto/ssh"
)

type ChannelHandler func(connectionDetails string, user *users.User, newChannel ssh.NewChannel, log logger.Logger)

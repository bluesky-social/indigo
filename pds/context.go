package pds

import (
	"context"

	"github.com/golang-jwt/jwt"
)

type (
	ctxAuth      struct{}
	ctxAuthScope struct{}
	ctxDID       struct{}
	ctxToken     struct{}
	ctxUser      struct{}
)

func withAuth(ctx context.Context, auth string) context.Context {
	return context.WithValue(ctx, ctxAuth{}, auth)
}

func getAuth(ctx context.Context) string {
	auth, _ := ctx.Value(ctxAuth{}).(string)
	return auth
}

func withAuthScope(ctx context.Context, scope string) context.Context {
	return context.WithValue(ctx, ctxAuthScope{}, scope)
}

func getAuthScope(ctx context.Context) string {
	scope, _ := ctx.Value(ctxAuthScope{}).(string)
	return scope
}

func withDID(ctx context.Context, did string) context.Context {
	return context.WithValue(ctx, ctxDID{}, did)
}

func getDID(ctx context.Context) string {
	did, _ := ctx.Value(ctxDID{}).(string)
	return did
}

func withToken(ctx context.Context, token *jwt.Token) context.Context {
	return context.WithValue(ctx, ctxToken{}, token)
}

func getToken(ctx context.Context) *jwt.Token {
	token, _ := ctx.Value(ctxToken{}).(*jwt.Token)
	return token
}

func withUser(ctx context.Context, user *User) context.Context {
	return context.WithValue(ctx, ctxUser{}, user)
}

func getUser(ctx context.Context) *User {
	user, _ := ctx.Value(ctxUser{}).(*User)
	return user
}

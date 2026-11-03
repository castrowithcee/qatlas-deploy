package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"

	"github.com/castrowithcee/qatlas-deploy/internal/control"
	"github.com/castrowithcee/qatlas-deploy/internal/credentials"
)

// decodeArgs reads the arguments strictly and answers with a fixed text, so a malformed request never echoes
// what it carried.
func decodeArgs(raw json.RawMessage, into any) error {
	if len(raw) == 0 {
		raw = json.RawMessage("{}")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(into); err != nil {
		return control.NewError(control.CodeBadRequest, "the arguments are not valid")
	}
	return nil
}

// answerOf turns a store error into the fixed answer of the channel; unknown errors stay internal.
func answerOf(err error) error {
	switch {
	case errors.Is(err, credentials.ErrInvalidName), errors.Is(err, credentials.ErrInvalidValue):
		return control.NewError(control.CodeInvalid, err.Error())
	case errors.Is(err, credentials.ErrKeyExists):
		return control.NewError(control.CodeKeyExists, err.Error())
	case errors.Is(err, credentials.ErrNoKey), errors.Is(err, credentials.ErrNotFound):
		return control.NewError(control.CodeNotFound, err.Error())
	}
	return err
}

func keyResult(k credentials.PublicKey) control.KeyResult {
	return control.KeyResult{PublicKey: k.Authorized, Fingerprint: k.Fingerprint}
}

func registerCredentialCommands(r *control.Registry, st *credentials.Store) error {
	commands := map[string]control.Handler{
		control.KeyGenerateCommand: func(_ context.Context, raw json.RawMessage) (any, error) {
			var a control.KeyGenerateArgs
			if err := decodeArgs(raw, &a); err != nil {
				return nil, err
			}
			k, err := st.GenerateKey(a.Replace)
			if err != nil {
				return nil, answerOf(err)
			}
			return keyResult(k), nil
		},
		control.KeyShowCommand: func(context.Context, json.RawMessage) (any, error) {
			k, err := st.ShowKey()
			if err != nil {
				return nil, answerOf(err)
			}
			return keyResult(k), nil
		},
		control.CredentialSetCommand: func(_ context.Context, raw json.RawMessage) (any, error) {
			var a control.CredentialSetArgs
			if err := decodeArgs(raw, &a); err != nil {
				return nil, err
			}
			if err := st.Set(a.Name, a.Value); err != nil {
				return nil, answerOf(err)
			}
			return struct{}{}, nil
		},
		control.CredentialListCommand: func(context.Context, json.RawMessage) (any, error) {
			entries, err := st.List()
			if err != nil {
				return nil, answerOf(err)
			}
			res := control.CredentialListResult{Credentials: make([]control.CredentialInfo, 0, len(entries))}
			for _, e := range entries {
				res.Credentials = append(res.Credentials, control.CredentialInfo{Name: e.Name, Modified: e.Modified})
			}
			return res, nil
		},
		control.CredentialDeleteCommand: func(_ context.Context, raw json.RawMessage) (any, error) {
			var a control.CredentialNameArgs
			if err := decodeArgs(raw, &a); err != nil {
				return nil, err
			}
			if err := st.Delete(a.Name); err != nil {
				return nil, answerOf(err)
			}
			return struct{}{}, nil
		},
	}
	for name, h := range commands {
		if err := r.Register(name, h); err != nil {
			return err
		}
	}
	return nil
}

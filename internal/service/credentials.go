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
	case errors.Is(err, credentials.ErrInvalidName), errors.Is(err, credentials.ErrInvalidValue),
		errors.Is(err, credentials.ErrKeyFormat), errors.Is(err, credentials.ErrKeyPassphrase),
		errors.Is(err, credentials.ErrKeyType), errors.Is(err, credentials.ErrKeyTooSmall),
		errors.Is(err, credentials.ErrKeyTooLarge):
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

// keyName names the deploy key of the stack folder dir, the default key for an empty dir.
func keyName(set *sourceSet, dir string) (string, error) {
	if dir == "" {
		return credentials.DefaultKey, nil
	}
	if err := checkDirShape(dir); err != nil {
		return "", control.NewError(control.CodeInvalid, err.Error())
	}
	conf, _, err := set.load()
	if err != nil {
		return "", err
	}
	name := keyFor(conf, dir)
	if !credentials.ValidKeyName(name) {
		return "", control.NewError(control.CodeInvalid, "the stack folder path is too long")
	}
	return name, nil
}

func registerCredentialCommands(r *control.Registry, st *credentials.Store, set *sourceSet) error {
	commands := map[string]control.Handler{
		control.KeyGenerateCommand: func(_ context.Context, raw json.RawMessage) (any, error) {
			var a control.KeyGenerateArgs
			if err := decodeArgs(raw, &a); err != nil {
				return nil, err
			}
			name, err := keyName(set, a.Dir)
			if err != nil {
				return nil, err
			}
			k, err := st.GenerateNamedKey(name, a.Replace)
			if err != nil {
				return nil, answerOf(err)
			}
			return keyResult(k), nil
		},
		control.KeySetCommand: func(_ context.Context, raw json.RawMessage) (any, error) {
			var a control.KeySetArgs
			if err := decodeArgs(raw, &a); err != nil {
				return nil, err
			}
			name, err := keyName(set, a.Dir)
			if err != nil {
				return nil, err
			}
			k, err := st.SetNamedKey(name, a.Key, a.Replace)
			if err != nil {
				return nil, answerOf(err)
			}
			return keyResult(k), nil
		},
		control.KeyShowCommand: func(_ context.Context, raw json.RawMessage) (any, error) {
			var a control.KeyShowArgs
			if err := decodeArgs(raw, &a); err != nil {
				return nil, err
			}
			name, err := keyName(set, a.Dir)
			if err != nil {
				return nil, err
			}
			k, err := st.ShowNamedKey(name)
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

package persistence

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
)

var ErrUncertainRotation = errors.New("previous credential refresh was not committed; sign in again to obtain new credentials")

type pendingRotation struct {
	RefreshTokenHash string `json:"refresh_token_sha256"`
	AccessTokenHash  string `json:"access_token_sha256"`
}

func (l *CredentialLock) BeginRotation(ctx context.Context, refreshToken, accessToken string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if refreshToken == "" {
		return errors.New("cannot rotate an empty refresh token")
	}
	pending, err := l.readRotation()
	if err != nil {
		return err
	}
	refreshHash := sha256.Sum256([]byte(refreshToken))
	accessHash := sha256.Sum256([]byte(accessToken))
	next := pendingRotation{hex.EncodeToString(refreshHash[:]), hex.EncodeToString(accessHash[:])}
	if pending != nil && *pending == next {
		return ErrUncertainRotation
	}
	if err := l.writeRotation(&next); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		// No token request has started, so this attempt is safe to discard.
		return errors.Join(err, l.CompleteRotation())
	}
	return nil
}

func (l *CredentialLock) CompleteRotation() error {
	return l.writeRotation(nil)
}

func (l *CredentialLock) readRotation() (*pendingRotation, error) {
	if _, err := l.file.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	body, err := io.ReadAll(io.LimitReader(l.file, 1025))
	if err != nil {
		return nil, err
	}
	if len(body) == 0 {
		return nil, nil // A newly created lock has no previous attempt.
	}
	if len(body) > 1024 {
		return nil, errors.New("credential rotation marker exceeds size limit")
	}
	var pending *pendingRotation
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&pending); err != nil {
		return nil, errors.New("credential rotation marker is invalid")
	}
	if decoder.Decode(new(json.RawMessage)) != io.EOF {
		return nil, errors.New("credential rotation marker has trailing data")
	}
	if pending != nil && (!validTokenHash(pending.RefreshTokenHash) || !validTokenHash(pending.AccessTokenHash)) {
		return nil, errors.New("credential rotation marker has invalid token fingerprints")
	}
	return pending, nil
}

func validTokenHash(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size && hex.EncodeToString(decoded) == value
}

func (l *CredentialLock) writeRotation(pending *pendingRotation) error {
	body, err := json.Marshal(pending)
	if err != nil {
		return err
	}
	if _, err := l.file.WriteAt(body, 0); err != nil {
		return err
	}
	if err := l.file.Truncate(int64(len(body))); err != nil {
		return err
	}
	return syncRotationMarker(l.file)
}

package errors

import (
	apierrs "k8s.io/apimachinery/pkg/api/errors"
)

func IgnoreNotFound(err error) error {
	if apierrs.IsNotFound(err) {
		return nil
	}
	return err
}

func IgnoreAlreadyExists(err error) error {
	if apierrs.IsAlreadyExists(err) {
		return nil
	}
	return err
}

func IgnoreConflict(err error) error {
	if apierrs.IsConflict(err) {
		return nil
	}
	return err
}

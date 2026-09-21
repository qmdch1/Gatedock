//go:build !windows

package platform

import "errors"

func PickKeyFile() (string, error) {
	return "", errors.New("파일 선택은 Windows에서 지원합니다. 파일 경로를 직접 입력해 주세요")
}

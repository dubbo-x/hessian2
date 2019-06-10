// Copyright 2016-2019 Alex Stocks
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package hessian

import (
	"bufio"
	"encoding/binary"
	"time"
)

import (
	perrors "github.com/pkg/errors"
)

// enum part
const (
	PackageError          = PackageType(0x01)
	PackageRequest        = PackageType(0x02)
	PackageResponse       = PackageType(0x04)
	PackageHeartbeat      = PackageType(0x08)
	PackageRequest_TwoWay = PackageType(0x10)
)

// PackageType ...
type PackageType int

type Header struct {
	MagicNumber    uint16
	HType          uint8
	ResponseStatus uint8
	ID             uint64
	BodyLen        uint32
}

func (header *Header) GetSerialID() uint8 {
	return header.HType & SERIAL_MASK
}

func (header *Header) SetSerialID(serialID uint8) {
	header.HType |= serialID
}

// DubboHeader dubbo header
type DubboHeader struct {
	Header
	Type PackageType
}

// Service defines service instance
type Service struct {
	Path      string
	Interface string
	Version   string
	Target    string // Service Name
	Method    string
	Timeout   time.Duration // request timeout
}

// HessianCodec defines hessian codec
type HessianCodec struct {
	pkgType PackageType
	reader  *bufio.Reader
	bodyLen int
}

// NewHessianCodec generate a new hessian codec instance
func NewHessianCodec(reader *bufio.Reader) *HessianCodec {
	return &HessianCodec{
		reader: reader,
	}
}

func (h *HessianCodec) Write(service Service, header DubboHeader, body interface{}) ([]byte, error) {
	switch header.Type {
	case PackageHeartbeat:
		if header.ResponseStatus == Zero {
			return packRequest(service, header, body)
		}
		return packResponse(header, map[string]string{}, body)
	case PackageRequest, PackageRequest_TwoWay:
		return packRequest(service, header, body)

	case PackageResponse:
		return packResponse(header, map[string]string{}, body)

	default:
		return nil, perrors.Errorf("Unrecognised message type: %v", header.Type)
	}

	// unreachable return nil, nil
}

// ReadHeader uses hessian codec to read dubbo header
func (h *HessianCodec) ReadHeader(header *DubboHeader) error {
	err := binary.Read(h.reader, binary.BigEndian, &header.Header)
	if err != nil {
		return perrors.WithStack(err)
	}

	flag := header.HType & FLAG_EVENT
	if flag != Zero {
		header.Type |= PackageHeartbeat
	}
	flag = header.HType & FLAG_REQUEST
	if flag != Zero {
		header.Type |= PackageRequest
		flag = header.HType & FLAG_TWOWAY
		if flag != Zero {
			header.Type |= PackageRequest_TwoWay
		}
	} else {
		header.Type |= PackageResponse

		// Header{status}
		if header.ResponseStatus != Response_OK {
			err = ErrJavaException
			header.Type |= PackageError
			bufSize := h.reader.Buffered()
			if bufSize > 1 {
				expBuf, expErr := h.reader.Peek(bufSize)
				if expErr == nil {
					err = perrors.Errorf("java exception:%s", string(expBuf[1:bufSize-1]))
				}
			}
		}
	}

	h.pkgType = header.Type
	h.bodyLen = int(header.BodyLen)

	return perrors.WithStack(err)

}

// ReadBody uses hessian codec to read response body
func (h *HessianCodec) ReadBody(rspObj interface{}) error {

	if h.reader.Buffered() < h.bodyLen {
		return ErrHeaderNotEnough
	}
	buf, err := h.reader.Peek(h.bodyLen)
	if err != nil {
		return perrors.WithStack(err)
	}
	_, err = h.reader.Discard(h.bodyLen)
	if err != nil { // this is impossible
		return perrors.WithStack(err)
	}

	switch h.pkgType & 0x0f {
	case PackageRequest | PackageHeartbeat, PackageResponse | PackageHeartbeat:
		return nil
	case PackageRequest:
		if rspObj != nil {
			if err = unpackRequestBody(buf, rspObj); err != nil {
				return perrors.WithStack(err)
			}
		}

		return nil

	case PackageResponse:
		if rspObj != nil {
			rsp, ok := rspObj.(*Response)
			if !ok {
				break
			}
			if err = unpackResponseBody(buf, rsp); err != nil {
				return perrors.WithStack(err)
			}
		}
	}

	return nil
}

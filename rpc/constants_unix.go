// Copyright 2019 The go-simplechain Authors
// This file is part of the go-simplechain library.
//
// The go-simplechain library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-simplechain library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-simplechain library. If not, see <http://www.gnu.org/licenses/>.

// +build darwin dragonfly freebsd linux nacl netbsd openbsd solaris

package rpc

/*
#include <sys/un.h>
#ifndef C_MAX_SOCKET_PATH_SIZE_H
#define C_MAX_SOCKET_PATH_SIZE_H
int max_socket_path_size() {
struct sockaddr_un s;
return sizeof(s.sun_path);
}
#endif //C_MAX_SOCKET_PATH_SIZE_H
*/
import "C"

var (
	max_path_size = C.max_socket_path_size()
)

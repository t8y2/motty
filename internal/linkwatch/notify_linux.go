package linkwatch

import "golang.org/x/sys/unix"

// openRouteSocket opens a netlink socket subscribed to links and addresses.
func openRouteSocket() (int, error) {
	socket, err := unix.Socket(unix.AF_NETLINK, unix.SOCK_RAW|unix.SOCK_CLOEXEC, unix.NETLINK_ROUTE)
	if err != nil {
		return -1, err
	}
	address := &unix.SockaddrNetlink{
		Family: unix.AF_NETLINK,
		Groups: unix.RTMGRP_LINK | unix.RTMGRP_IPV4_IFADDR | unix.RTMGRP_IPV6_IFADDR,
	}
	if err := unix.Bind(socket, address); err != nil {
		unix.Close(socket)
		return -1, err
	}
	return socket, nil
}

#!/usr/bin/env python
# Copyright 2025 NetBox Labs Inc
"""Async TCP port scanning helpers and hostname expansion."""

import ipaddress
import socket
from collections.abc import Iterable
from concurrent.futures import ThreadPoolExecutor, as_completed


def expand_hostnames(hostname: str) -> tuple[list[str], bool]:
    """Expand hostname into a list of addresses; return parsed_as_range flag."""
    sanitized_hostname = hostname.strip()

    if "-" in sanitized_hostname:
        try:
            start, end = sanitized_hostname.split("-", 1)
            start_ip = ipaddress.ip_address(start.strip())
            end_ip = ipaddress.ip_address(end.strip())
        except ValueError:
            return [sanitized_hostname], False

        start_int, end_int = sorted((int(start_ip), int(end_ip)))
        hosts = [
            str(ipaddress.ip_address(ip_int)) for ip_int in range(start_int, end_int + 1)
        ]
        return hosts, True

    if "/" in sanitized_hostname:
        try:
            network = ipaddress.ip_network(sanitized_hostname, strict=False)
        except ValueError:
            return [sanitized_hostname], False

        hosts = [str(ip) for ip in network.hosts()]
        if not hosts:
            hosts = [str(network.network_address)]
        return hosts, True

    return [sanitized_hostname], False


def _probe_port(hostname: str, port: int, timeout: float) -> bool:
    """Return True if the TCP port is reachable using sockets."""
    try:
        with socket.create_connection((hostname, port), timeout=timeout):
            return True
    except OSError:
        return False


def has_reachable_port(hostname: str, ports: Iterable[int], timeout: float) -> bool:
    """
    Check if any of the given TCP ports are reachable.

    Runs socket connects in a thread pool so it works even when an asyncio loop
    is already running.
    """
    port_list = list(dict.fromkeys(ports))
    if not port_list:
        return False

    worker_count = min(len(port_list), 64)
    with ThreadPoolExecutor(max_workers=worker_count) as executor:
        futures = [
            executor.submit(_probe_port, hostname, port, timeout)
            for port in port_list
        ]
        for future in as_completed(futures):
            try:
                if future.result():
                    return True
            except Exception:
                continue
    return False

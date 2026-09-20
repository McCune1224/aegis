import random
import socket
import sys

# Sends one A query for example.com and expects a DNS answer, so a container
# image proves it resolves end to end before CI calls it good.
host, port = sys.argv[1], int(sys.argv[2])
query = bytearray(b"\x00\x00\x01\x00\x00\x01\x00\x00\x00\x00\x00\x00")
query[0:2] = random.randrange(65536).to_bytes(2, "big")
for label in "example.com".split("."):
    query += bytes([len(label)]) + label.encode()
query += b"\x00\x00\x01\x00\x01"

sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
sock.settimeout(8)
sock.sendto(bytes(query), (host, port))
response, _ = sock.recvfrom(512)
assert response[3] & 0x0F == 0, f"answer did not resolve: rcode {response[3] & 0x0F}"
print("answered")

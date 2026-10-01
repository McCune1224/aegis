// A stub upstream resolver. A rootless network namespace has no route off the
// box, so the three public resolvers aegis is pointed at are loopback aliases
// inside it and this answers for them. Everything above the wire is real: the
// resolver, the filter, the log, and the verdicts. Only the answer is a stub,
// which is the tier 2 pattern in docs/testing.md.

import dgram from "node:dgram";

const TYPE_A = 1;
const TYPE_CNAME = 5;
const TYPE_AAAA = 28;
const CLASS_IN = 1;

function readName(message, offset) {
  let at = offset;
  while (at < message.length && message.at(at) !== 0) {
    at += message.at(at) + 1;
  }
  return { end: at + 1 };
}

// answer replies to one query with a plausible address so the resolver has
// something real to cache and hand back.
export function respond(message, remote, delayMs) {
  const id = message.readUInt16BE(0);
  const flags = message.readUInt16BE(2);
  const question = readName(message, 12);
  const qtype = message.readUInt16BE(question.end);
  const qclass = message.readUInt16BE(question.end + 2);
  const questionBytes = message.subarray(12, question.end + 4);

  const records = [];
  if (qclass === CLASS_IN && qtype === TYPE_A) {
    records.push({ type: TYPE_A, ttl: 300, rdata: Buffer.from([93, 184, 216, 34]) });
  } else if (qclass === CLASS_IN && qtype === TYPE_AAAA) {
    records.push({ type: TYPE_AAAA, ttl: 300, rdata: Buffer.from("2606280022010248189325c81946", "hex") });
  } else if (qtype === TYPE_CNAME) {
    records.push({ type: TYPE_CNAME, ttl: 60, rdata: Buffer.from([12, 108, 112, 120, 108, 101, 120, 116, 101, 100, 121, 111, 117, 116, 117, 98, 101, 99, 111, 109, 3, 99, 111, 109, 0]) });
  }

  const header = Buffer.alloc(12);
  header.writeUInt16BE(id, 0);
  header.writeUInt16BE(0x8180 | (flags & 0x0100), 2);
  header.writeUInt16BE(1, 4);
  header.writeUInt16BE(records.length, 6);

  const answers = [];
  for (const record of records) {
    const head = Buffer.alloc(12);
    head.writeUInt16BE(0xc00c, 0);
    head.writeUInt16BE(record.type, 2);
    head.writeUInt16BE(CLASS_IN, 4);
    head.writeUInt32BE(record.ttl, 6);
    head.writeUInt16BE(record.rdata.length, 8);
    answers.push(head, record.rdata);
  }
  const payload = Buffer.concat([header, questionBytes, ...answers]);

  setTimeout(() => {
    if (payload.length <= 512) {
      socket.send(payload, remote.port, remote.address);
      return;
    }
    // Truncation is answered over TCP so the resolver exercises that path too.
    const short = Buffer.from(payload);
    short.writeUInt16BE(0x8183, 2);
    short.writeUInt16BE(0, 6);
    socket.send(short.subarray(0, questionBytes.length + 12), remote.port, remote.address);
  }, delayMs);
}

const PORT = Number(process.env.STUB_DNS_PORT ?? 15353);
const DELAY = Number(process.env.STUB_DNS_DELAY ?? 4);
const socket = dgram.createSocket({ type: "udp4", reuseAddr: true });

socket.on("message", (message, remote) => respond(message, remote, DELAY));
socket.on("error", (error) => {
  process.stderr.write(`stub upstream: ${error.message}\n`);
  process.exit(1);
});
socket.bind(PORT, "0.0.0.0", () => {
  process.stdout.write(`stub upstream answering on 0.0.0.0:${PORT}\n`);
});
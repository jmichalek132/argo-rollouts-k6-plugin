import http from "k6/http";
import { check, sleep } from "k6";

// k6-plugin-demo-fail.js — intentionally failing thresholds for e2e failure tests.
// The p(95) threshold of 1ms is impossible to meet; the test always fails.
export const options = {
  cloud: {
    name: "k6-plugin-demo-fail",
  },
  vus: 1,
  duration: "10s",
  thresholds: {
    http_req_duration: ["p(95)<1"],  // 1ms — impossible, always fails
  },
};

export default function () {
  const res = http.get("https://test.k6.io");
  check(res, { "status is 200": (r) => r.status === 200 });
  sleep(1);
}

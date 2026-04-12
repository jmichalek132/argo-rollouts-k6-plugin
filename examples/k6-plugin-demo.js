import http from "k6/http";
import { check, sleep } from "k6";

export const options = {
  cloud: {
    projectID: "<YOUR_PROJECT_ID>",
    name: "k6-plugin-demo",
  },
  vus: 1,
  duration: "10s",
  thresholds: {
    http_req_failed: ["rate<0.05"],
    http_req_duration: ["p(95)<2000"],
  },
};

export default function () {
  const res = http.get("https://test.k6.io");
  check(res, { "status is 200": (r) => r.status === 200 });
  sleep(1);
}

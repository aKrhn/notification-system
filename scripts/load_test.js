import http from 'k6/http';
import { check, sleep } from 'k6';

export const options = {
    stages: [
        { duration: '30s', target: 50 },   // Ramp up to 50 concurrent users
        { duration: '1m',  target: 100 },  // Hold at 100
        { duration: '30s', target: 300 },  // Burst — simulate flash sale
        { duration: '30s', target: 0 },    // Ramp down
    ],
    thresholds: {
        http_req_duration: ['p(95)<500'],  // 95% of requests under 500ms
        http_req_failed: ['rate<0.01'],    // Less than 1% error rate
    },
};

const channels = ['sms', 'email', 'push'];
const priorities = ['high', 'normal', 'low'];

export default function () {
    const channel = channels[Math.floor(Math.random() * channels.length)];
    const priority = priorities[Math.floor(Math.random() * priorities.length)];

    const payload = JSON.stringify({
        recipient: `+9055512${Math.floor(Math.random() * 100000).toString().padStart(5, '0')}`,
        channel: channel,
        content: `Load test notification - ${Date.now()}`,
        priority: priority,
        subject: channel === 'email' ? 'Load Test' : undefined,
    });

    const res = http.post('http://localhost:8080/api/v1/notifications', payload, {
        headers: { 'Content-Type': 'application/json' },
    });

    check(res, {
        'status is 201': (r) => r.status === 201,
        'response time < 200ms': (r) => r.timings.duration < 200,
        'has id': (r) => JSON.parse(r.body).id !== undefined,
    });

    sleep(0.1); // Brief pause between requests
}

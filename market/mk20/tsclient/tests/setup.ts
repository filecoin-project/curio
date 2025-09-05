// Global test setup
import 'isomorphic-fetch';

// Mock fetch for testing
global.fetch = jest.fn();

// Reset mocks before each test
beforeEach(() => {
  jest.clearAllMocks();
});

// Test utilities
export const mockResponse = (data: any, status = 200) => {
  return Promise.resolve({
    ok: status >= 200 && status < 300,
    status,
    json: () => Promise.resolve(data),
    text: () => Promise.resolve(JSON.stringify(data)),
  } as Response);
};

export const mockError = (status: number, message: string) => {
  return Promise.reject(new Error(`${status}: ${message}`));
};

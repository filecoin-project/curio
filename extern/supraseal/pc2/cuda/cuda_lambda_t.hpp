// Copyright Supranational LLC

#ifndef __CUDA_LAMBDA_T_HPP__
#define __CUDA_LAMBDA_T_HPP__

#if __cplusplus < 201103L && !(defined(_MSVC_LANG) && _MSVC_LANG >= 201103L)
# error C++11 or later is required.
#endif
#include <functional>
#include <map>
#include <mutex>
#include <assert.h>

// Lambda function execution from a cuda stream. Work scheduled using
// 'schedule' will be inserted into the stream queue and executed
// in a separate thread when triggered.
class cuda_lambda_t {
    typedef std::function<void()> job_t;
    thread_pool_t pool;

    struct work_item_t {
        job_t work;
        cuda_lambda_t* me;
    };
    
    static void cb(void *userData) {
        work_item_t* work = (work_item_t*)userData;
        work->me->pool.spawn([work]() {
            work->work();
            delete work;
        });
    }

public:
    cuda_lambda_t(size_t num_threads = 0) :
        pool(num_threads)
    {}

    template<class Workable>
    void schedule(stream_t &stream, Workable work) {
        work_item_t* new_work = new work_item_t;
        new_work->me = this;
        new_work->work = work;
        cudaLaunchHostFunc(stream, cb, new_work);
    }
};

#endif

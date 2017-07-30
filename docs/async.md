# Asynchronous Functions

Asynchronous (async) functions will run your function at some point in the future. The default mode is synchronous which means
a function is executed and the caller blocks while waiting for the response. Asynchronous on the other, puts the request into a 
message queue and responds immediately to the caller. The function will then be executed at some point in the future, upon resource availability giving priority to synchronous calls. 

Asynchronous function calls are great for tasks that are CPU heavy or take more than a few seconds to complete.
For instance, image processing, video processing, data processing, ETL, etc. Also, since it is using a message queue, you can safely queue up millions of function calls without worrying about capacity. 

## Enabling Hot Functions

To enable hot functions, set the `type` parameter in your `func.yaml`:

```yaml
type: async
```

Or pass in the `format` parameter when creating or updating a route if using the API directory. 

## Responses

Async functions will return immediately with a `call_id`, for example:

```json
{"call_id": "abc123"}
```

The `call_id` can then be used to retrieve the status at a later time. 


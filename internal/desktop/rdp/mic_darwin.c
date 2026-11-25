//go:build darwin && !ios

#include <AudioToolbox/AudioToolbox.h>
#include <CoreAudio/CoreAudio.h>
#include <pthread.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>

typedef struct motty_mic motty_mic;

struct motty_mic {
	pthread_mutex_t mutex;
	pthread_cond_t cond;
	uint8_t *data;
	size_t capacity;
	size_t frame;
	size_t read;
	size_t used;
	AudioUnit unit;
	AudioConverterRef converter;
	AudioStreamBasicDescription client_fmt;
	AudioStreamBasicDescription target_fmt;
	uint8_t *capture_buf;
	size_t capture_cap;
	uint8_t *convert_buf;
	size_t convert_cap;
	const uint8_t *conv_ptr;
	UInt32 conv_bytes;
	int started;
	int closed;
};

static size_t motty_min(size_t a, size_t b) { return a < b ? a : b; }

static void motty_ring_push(struct motty_mic *m, const uint8_t *src, size_t len) {
	if (len == 0) {
		return;
	}
	pthread_mutex_lock(&m->mutex);
	if (m->closed) {
		pthread_mutex_unlock(&m->mutex);
		return;
	}
	if (len >= m->capacity) {
		src += len - m->capacity;
		len = m->capacity;
		m->read = 0;
		m->used = 0;
	} else if (m->used + len > m->capacity) {
		size_t drop = m->used + len - m->capacity;
		m->read = (m->read + drop) % m->capacity;
		m->used -= drop;
	}
	size_t write = (m->read + m->used) % m->capacity;
	size_t first = motty_min(len, m->capacity - write);
	memcpy(m->data + write, src, first);
	if (len > first) {
		memcpy(m->data, src + first, len - first);
	}
	m->used += len;
	pthread_cond_signal(&m->cond);
	pthread_mutex_unlock(&m->mutex);
}

static OSStatus motty_conv_supply(AudioConverterRef converter, UInt32 *packets,
                                  AudioBufferList *data,
                                  AudioStreamPacketDescription **desc,
                                  void *ref) {
	(void)converter;
	(void)desc;
	struct motty_mic *m = ref;
	if (m->conv_bytes == 0) {
		*packets = 0;
		data->mBuffers[0].mDataByteSize = 0;
		data->mBuffers[0].mData = NULL;
		return noErr;
	}
	UInt32 avail = m->conv_bytes / m->client_fmt.mBytesPerPacket;
	UInt32 take = *packets < avail ? *packets : avail;
	UInt32 take_bytes = take * m->client_fmt.mBytesPerPacket;
	data->mNumberBuffers = 1;
	data->mBuffers[0].mNumberChannels = m->client_fmt.mChannelsPerFrame;
	data->mBuffers[0].mData = (void *)m->conv_ptr;
	data->mBuffers[0].mDataByteSize = take_bytes;
	m->conv_ptr += take_bytes;
	m->conv_bytes -= take_bytes;
	*packets = take;
	return noErr;
}

static OSStatus motty_mic_input(void *ref, AudioUnitRenderActionFlags *flags,
                                const AudioTimeStamp *timestamp, UInt32 bus,
                                UInt32 frames, AudioBufferList *io_data) {
	(void)io_data;
	struct motty_mic *m = ref;
	size_t need = (size_t)frames * m->client_fmt.mBytesPerFrame;
	if (need == 0 || need > m->capture_cap) {
		return noErr;
	}

	AudioBufferList abl;
	abl.mNumberBuffers = 1;
	abl.mBuffers[0].mNumberChannels = m->client_fmt.mChannelsPerFrame;
	abl.mBuffers[0].mDataByteSize = (UInt32)need;
	abl.mBuffers[0].mData = m->capture_buf;

	OSStatus err = AudioUnitRender(m->unit, flags, timestamp, bus, frames, &abl);
	if (err != noErr) {
		return err;
	}

	if (m->converter == NULL) {
		motty_ring_push(m, m->capture_buf, abl.mBuffers[0].mDataByteSize);
		return noErr;
	}

	m->conv_ptr = m->capture_buf;
	m->conv_bytes = abl.mBuffers[0].mDataByteSize;
	while (m->conv_bytes > 0) {
		UInt32 out_packets = (UInt32)(m->convert_cap / m->target_fmt.mBytesPerPacket);
		AudioBufferList out_abl;
		out_abl.mNumberBuffers = 1;
		out_abl.mBuffers[0].mNumberChannels = m->target_fmt.mChannelsPerFrame;
		out_abl.mBuffers[0].mDataByteSize = out_packets * m->target_fmt.mBytesPerPacket;
		out_abl.mBuffers[0].mData = m->convert_buf;

		err = AudioConverterFillComplexBuffer(m->converter, motty_conv_supply, m,
		                                      &out_packets, &out_abl, NULL);
		if (out_packets > 0 && out_abl.mBuffers[0].mDataByteSize > 0) {
			motty_ring_push(m, m->convert_buf, out_abl.mBuffers[0].mDataByteSize);
		}
		if (err != noErr || out_packets == 0) {
			break;
		}
	}
	return noErr;
}

motty_mic *motty_mic_open(double rate, uint32_t channels, int32_t *status) {
	*status = 0;
	if (rate < 8000 || rate > 192000 || channels < 1 || channels > 2) {
		*status = kAudioUnitErr_InvalidParameter;
		return NULL;
	}

	AudioObjectPropertyAddress dev_addr = {
		.mSelector = kAudioHardwarePropertyDefaultInputDevice,
		.mScope = kAudioObjectPropertyScopeGlobal,
		.mElement = kAudioObjectPropertyElementMain,
	};
	AudioDeviceID dev_id = kAudioObjectUnknown;
	UInt32 dev_size = sizeof(dev_id);
	OSStatus err = AudioObjectGetPropertyData(kAudioObjectSystemObject, &dev_addr, 0, NULL, &dev_size, &dev_id);
	if (err != noErr || dev_id == kAudioObjectUnknown) {
		*status = -1;
		return NULL;
	}

	AudioComponentDescription description = {
		.componentType = kAudioUnitType_Output,
		.componentSubType = kAudioUnitSubType_HALOutput,
		.componentManufacturer = kAudioUnitManufacturer_Apple,
	};
	AudioComponent component = AudioComponentFindNext(NULL, &description);
	if (component == NULL) {
		*status = -1;
		return NULL;
	}

	struct motty_mic *m = calloc(1, sizeof(*m));
	if (m == NULL) {
		*status = kAudioUnitErr_NoConnection;
		return NULL;
	}
	m->frame = (size_t)channels * 2;
	m->capacity = (size_t)(rate / 4.0) * m->frame;
	if (m->capacity < 4096) {
		m->capacity = 4096;
	}
	m->capacity -= m->capacity % m->frame;
	m->data = malloc(m->capacity);
	m->capture_cap = 65536;
	m->capture_buf = malloc(m->capture_cap);
	m->convert_cap = 65536;
	m->convert_buf = malloc(m->convert_cap);
	if (m->data == NULL || m->capture_buf == NULL || m->convert_buf == NULL) {
		free(m->data);
		free(m->capture_buf);
		free(m->convert_buf);
		free(m);
		*status = kAudioUnitErr_NoConnection;
		return NULL;
	}
	if (pthread_mutex_init(&m->mutex, NULL) != 0 ||
	    pthread_cond_init(&m->cond, NULL) != 0) {
		free(m->data);
		free(m->capture_buf);
		free(m->convert_buf);
		free(m);
		*status = kAudioUnitErr_NoConnection;
		return NULL;
	}

	err = AudioComponentInstanceNew(component, &m->unit);
	if (err != noErr) {
		goto fail;
	}

	UInt32 enable_in = 1;
	err = AudioUnitSetProperty(m->unit, kAudioOutputUnitProperty_EnableIO,
	                           kAudioUnitScope_Input, 1, &enable_in, sizeof(enable_in));
	if (err != noErr) {
		goto fail;
	}

	UInt32 disable_out = 0;
	err = AudioUnitSetProperty(m->unit, kAudioOutputUnitProperty_EnableIO,
	                           kAudioUnitScope_Output, 0, &disable_out, sizeof(disable_out));
	if (err != noErr) {
		goto fail;
	}

	err = AudioUnitSetProperty(m->unit, kAudioOutputUnitProperty_CurrentDevice,
	                           kAudioUnitScope_Global, 0, &dev_id, sizeof(dev_id));
	if (err != noErr) {
		goto fail;
	}

	AudioStreamBasicDescription hw_fmt;
	UInt32 hw_size = sizeof(hw_fmt);
	err = AudioUnitGetProperty(m->unit, kAudioUnitProperty_StreamFormat,
	                           kAudioUnitScope_Input, 1, &hw_fmt, &hw_size);
	if (err != noErr) {
		goto fail;
	}

	UInt32 hw_channels = hw_fmt.mChannelsPerFrame;
	if (hw_channels == 0) {
		hw_channels = channels;
	}
	if (hw_channels > 2) {
		hw_channels = 2;
	}
	double hw_rate = hw_fmt.mSampleRate > 0 ? hw_fmt.mSampleRate : rate;

	m->client_fmt = (AudioStreamBasicDescription){
		.mSampleRate = hw_rate,
		.mFormatID = kAudioFormatLinearPCM,
		.mFormatFlags = kLinearPCMFormatFlagIsSignedInteger | kLinearPCMFormatFlagIsPacked,
		.mBytesPerPacket = hw_channels * 2,
		.mFramesPerPacket = 1,
		.mBytesPerFrame = hw_channels * 2,
		.mChannelsPerFrame = hw_channels,
		.mBitsPerChannel = 16,
	};
	m->target_fmt = (AudioStreamBasicDescription){
		.mSampleRate = rate,
		.mFormatID = kAudioFormatLinearPCM,
		.mFormatFlags = kLinearPCMFormatFlagIsSignedInteger | kLinearPCMFormatFlagIsPacked,
		.mBytesPerPacket = (UInt32)m->frame,
		.mFramesPerPacket = 1,
		.mBytesPerFrame = (UInt32)m->frame,
		.mChannelsPerFrame = channels,
		.mBitsPerChannel = 16,
	};

	err = AudioUnitSetProperty(m->unit, kAudioUnitProperty_StreamFormat,
	                           kAudioUnitScope_Output, 1, &m->client_fmt, sizeof(m->client_fmt));
	if (err != noErr) {
		goto fail;
	}

	if (m->client_fmt.mSampleRate != m->target_fmt.mSampleRate ||
	    m->client_fmt.mChannelsPerFrame != m->target_fmt.mChannelsPerFrame) {
		err = AudioConverterNew(&m->client_fmt, &m->target_fmt, &m->converter);
		if (err != noErr) {
			goto fail;
		}
	}

	AURenderCallbackStruct callback = {
		.inputProc = motty_mic_input,
		.inputProcRefCon = m,
	};
	err = AudioUnitSetProperty(m->unit, kAudioOutputUnitProperty_SetInputCallback,
	                           kAudioUnitScope_Global, 0, &callback, sizeof(callback));
	if (err != noErr) {
		goto fail;
	}

	err = AudioUnitInitialize(m->unit);
	if (err != noErr) {
		goto fail;
	}
	err = AudioOutputUnitStart(m->unit);
	if (err != noErr) {
		AudioUnitUninitialize(m->unit);
		goto fail;
	}
	m->started = 1;
	return m;

fail:
	*status = err;
	if (m->converter != NULL) {
		AudioConverterDispose(m->converter);
	}
	if (m->unit != NULL) {
		AudioComponentInstanceDispose(m->unit);
	}
	pthread_mutex_destroy(&m->mutex);
	pthread_cond_destroy(&m->cond);
	free(m->data);
	free(m->capture_buf);
	free(m->convert_buf);
	free(m);
	return NULL;
}

int64_t motty_mic_read(motty_mic *m, void *pcm, uintptr_t len) {
	if (m == NULL || len == 0) {
		return 0;
	}
	pthread_mutex_lock(&m->mutex);
	while (m->used == 0 && !m->closed) {
		pthread_cond_wait(&m->cond, &m->mutex);
	}
	if (m->closed) {
		pthread_mutex_unlock(&m->mutex);
		return 0;
	}
	size_t take = motty_min((size_t)len, m->used);
	take -= take % m->frame;
	if (take == 0 && m->used > 0) {
		take = motty_min((size_t)len, m->used);
	}
	uint8_t *dst = pcm;
	size_t first = motty_min(take, m->capacity - m->read);
	memcpy(dst, m->data + m->read, first);
	if (take > first) {
		memcpy(dst + first, m->data, take - first);
	}
	m->read = (m->read + take) % m->capacity;
	m->used -= take;
	pthread_mutex_unlock(&m->mutex);
	return (int64_t)take;
}

void motty_mic_stop(motty_mic *m) {
	if (m == NULL) {
		return;
	}
	pthread_mutex_lock(&m->mutex);
	m->closed = 1;
	pthread_cond_broadcast(&m->cond);
	pthread_mutex_unlock(&m->mutex);
}

void motty_mic_close(motty_mic *m) {
	if (m == NULL) {
		return;
	}
	motty_mic_stop(m);
	if (m->started) {
		AudioOutputUnitStop(m->unit);
		m->started = 0;
	}
	AudioUnitUninitialize(m->unit);
	AudioComponentInstanceDispose(m->unit);
	if (m->converter != NULL) {
		AudioConverterDispose(m->converter);
	}
	pthread_mutex_destroy(&m->mutex);
	pthread_cond_destroy(&m->cond);
	free(m->data);
	free(m->capture_buf);
	free(m->convert_buf);
	free(m);
}

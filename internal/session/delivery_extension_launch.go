package session

// setLaunchDeliveryExtension hands a launch the pi extension the pre-launch
// hook wired for this session. It is set on every launch, also when empty: the
// backend is shared.
func setLaunchDeliveryExtension(backend any, sess *Session) {
	if b, ok := backend.(interface{ SetDeliveryExtension(string) }); ok {
		b.SetDeliveryExtension(sess.DeliveryExtension)
	}
}

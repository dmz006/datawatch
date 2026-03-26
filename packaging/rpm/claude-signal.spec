Name:           claude-signal
Version:        0.1.0
Release:        1%{?dist}
Summary:        Signal to Claude Code bridge daemon

License:        Polyform-Noncommercial-1.0
URL:            https://github.com/dmz006/claude-signal
Source0:        https://github.com/dmz006/claude-signal/archive/v%{version}.tar.gz

BuildRequires:  golang >= 1.22
Requires:       java-17-openjdk-headless
Requires:       tmux >= 3.0

%description
claude-signal bridges Signal messenger group messages to claude-code
AI coding sessions running in tmux. Message yourself to start coding
tasks, receive async updates, and interact with sessions from anywhere.

%prep
%autosetup -n claude-signal-%{version}

%build
go build -ldflags="-X main.Version=%{version}" \
  -o claude-signal ./cmd/claude-signal/

%install
install -Dm755 claude-signal %{buildroot}%{_bindir}/claude-signal
install -Dm644 install/systemd/claude-signal.service \
  %{buildroot}%{_unitdir}/claude-signal.service
install -dm755 %{buildroot}%{_sysconfdir}/claude-signal
install -dm755 %{buildroot}%{_sharedstatedir}/claude-signal

%pre
getent group claude-signal >/dev/null || groupadd -r claude-signal
getent passwd claude-signal >/dev/null || \
  useradd -r -g claude-signal -d %{_sharedstatedir}/claude-signal \
    -s /sbin/nologin -c "Claude Signal daemon" claude-signal
exit 0

%post
%systemd_post claude-signal.service

%preun
%systemd_preun claude-signal.service

%postun
%systemd_postun_with_restart claude-signal.service

%files
%license LICENSE
%doc README.md
%{_bindir}/claude-signal
%{_unitdir}/claude-signal.service
%dir %{_sysconfdir}/claude-signal
%attr(0750,claude-signal,claude-signal) %dir %{_sharedstatedir}/claude-signal

%changelog
* Wed Jan 01 2025 dmz006 <dmz006@github.com> - 0.1.0-1
- Initial package release
